package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Name   string  `yaml:"name"`
	Token  string  `yaml:"token"`
	Secret string  `yaml:"secret"`
	Survey []Slide `yaml:"survey"`
}

type Slide struct {
	Type       string   `yaml:"type"`
	Question   string   `yaml:"question"`
	ResultType string   `yaml:"result"`
	Answers    []string `yaml:"answers,omitempty"`
}

var (
	config        Config
	currentSlide  int = -1
	answers       sync.Map
	clients       = make(map[*websocket.Conn]bool)
	broadcast     = make(chan Message)
	upgrader      = websocket.Upgrader{}
	userResponses sync.Map
)

const (
	userIDCookieName = "survey_user_id"
	cookieMaxAge     = 2 * 60 * 60 // 24 hours in seconds
)

func generateUserID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func getUserID(c echo.Context) (string, error) {
	cookie, err := c.Cookie(userIDCookieName)
	if err == nil {
		return cookie.Value, nil
	}

	userID, err := generateUserID()
	if err != nil {
		return "", err
	}

	cookie = &http.Cookie{
		Name:     userIDCookieName,
		Value:    userID,
		MaxAge:   cookieMaxAge,
		Expires:  time.Now().Add(cookieMaxAge * time.Second),
		HttpOnly: true,
		Secure:   c.Request().TLS != nil, // Set to true if using HTTPS
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	}
	c.SetCookie(cookie)

	return userID, nil
}

type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

func main() {
	loadConfig("config.yaml")

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.Static("/static", "static")

	t := template.Must(template.ParseGlob("views/*.html"))
	template.Must(t.ParseGlob("views/_layout/*.html"))
	template.Must(t.ParseGlob("views/components/*.html"))
	e.Renderer = &TemplateRenderer{
		templates: t,
	}

	e.GET("/", handleIndex)
	e.GET("/survey/:token", handleSurvey)
	e.POST("/submit/:token", handleSubmit)
	e.GET("/results/:token", handleResults)
	e.GET("/completed/:token", handleCompleted)
	e.GET("/ws", handleWebSocket)
	e.POST("/nextSlide", handleNextSlide)

	go handleMessages()

	e.Logger.Fatal(e.Start(":8000"))
}

type TemplateRenderer struct {
	templates *template.Template
}

func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func loadConfig(filename string) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}
}

func handleIndex(c echo.Context) error {
	return c.Render(http.StatusOK, "index.html", nil)
}

func handleCompleted(c echo.Context) error {
	token := c.Param("token")
	if token != config.Token {
		return c.String(http.StatusUnauthorized, "Invalid token")
	}

	if currentSlide >= len(config.Survey) {
		return c.Render(http.StatusOK, "completed.html", nil)
	}

	if currentSlide == 0 {
		return c.Render(http.StatusOK, "waiting.html", map[string]interface{}{
			"UserCount":  len(clients),
			"SurveyName": config.Name,
		})
	}

	clearUserIDCookie(c)

	return c.Redirect(http.StatusSeeOther, "/survey/"+token)
}

func handleSurvey(c echo.Context) error {
	token := c.Param("token")
	if token != config.Token {
		return c.String(http.StatusUnauthorized, "Invalid token")
	}

	userID, err := getUserID(c)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Error generating user ID")
	}

	if currentSlide >= len(config.Survey) {
		return c.Render(http.StatusOK, "completed.html", nil)
	}

	if currentSlide == -1 {
		return c.Render(http.StatusOK, "waiting.html", map[string]interface{}{
			"UserCount":  len(clients),
			"SurveyName": config.Name,
		})
	}

	// Check if the user has already answered this slide
	if hasUserAnswered(token, currentSlide, userID) {
		return c.Redirect(http.StatusSeeOther, fmt.Sprintf("/results/%s", token)) //this never works
	}

	slide := config.Survey[currentSlide]
	return c.Render(http.StatusOK, "survey.html", map[string]interface{}{
		"Slide": slide,
		"Token": token,
	})
}

func clearUserIDCookie(c echo.Context) {
	cookie := &http.Cookie{
		Name:     userIDCookieName,
		Value:    "",
		MaxAge:   -1,
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
		Secure:   c.Request().TLS != nil,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	}
	c.SetCookie(cookie)
}

func handleSubmit(c echo.Context) error {
	token := c.Param("token")
	if token != config.Token {
		return c.String(http.StatusUnauthorized, "Invalid token")
	}

	userID, err := getUserID(c)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Error generating user ID")
	}

	if hasUserAnswered(token, currentSlide, userID) {
		return c.Redirect(http.StatusSeeOther, fmt.Sprintf("/results/%s", token)) //this works
	}

	slide := config.Survey[currentSlide]
	var selectedAnswers []string

	if slide.Type == "multiple" {
		err := c.Request().ParseForm()
		if err != nil {
			return c.String(http.StatusBadRequest, "Error parsing form data")
		}

		selectedAnswers = c.Request().Form["answers"]

		if len(selectedAnswers) == 0 {
			body, err := ioutil.ReadAll(c.Request().Body)
			if err != nil {
				return c.String(http.StatusBadRequest, "Error reading request body")
			}

			bodyStr := string(body)
			pairs := strings.Split(bodyStr, "&")
			for _, pair := range pairs {
				kv := strings.Split(pair, "=")
				if len(kv) == 2 && kv[0] == "answers" {
					selectedAnswers = append(selectedAnswers, kv[1])
				}
			}
		}
	} else {
		answer := c.FormValue("answer")
		selectedAnswers = []string{answer}
	}

	storeAnswers(token, currentSlide, userID, selectedAnswers)

	results := getResults(token)
	broadcast <- Message{Type: "newAnswer", Payload: results}

	return c.Redirect(http.StatusSeeOther, "/results/"+token)
}

func storeAnswers(token string, slide int, userID string, newAnswers []string) {
	key := fmt.Sprintf("%s:%d", token, slide)
	if existingAns, ok := answers.Load(key); ok {
		answers.Store(key, append(existingAns.([]string), newAnswers...))
	} else {
		answers.Store(key, newAnswers)
	}

	// Store user response
	userKey := fmt.Sprintf("%s:%d:%s", token, slide, userID)
	userResponses.Store(userKey, true)
}

func hasUserAnswered(token string, slide int, userID string) bool {
	userKey := fmt.Sprintf("%s:%d:%s", token, slide, userID)
	_, answered := userResponses.Load(userKey)
	return answered
}

func getResults(token string) map[string]int {
	results := make(map[string]int)
	key := fmt.Sprintf("%s:%d", token, currentSlide)
	if slideAnswers, ok := answers.Load(key); ok {
		for _, answer := range slideAnswers.([]string) {
			results[answer]++
		}
	}
	return results
}

func handleResults(c echo.Context) error {
	token := c.Param("token")
	if token != config.Token {
		return c.String(http.StatusUnauthorized, "Invalid token")
	}

	userID, err := getUserID(c)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Error retrieving user ID")
	}

	results := getResults(token)
	hasAnswered := hasUserAnswered(token, currentSlide, userID)

	return c.Render(http.StatusOK, "results.html", map[string]interface{}{
		"Slide":       config.Survey[currentSlide],
		"Results":     results,
		"HasAnswered": hasAnswered,
	})
}

func handleWebSocket(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}

	clients[ws] = true

	broadcast <- Message{Type: "userCount", Payload: len(clients)}

	defer func() {
		delete(clients, ws)
		ws.Close()
		broadcast <- Message{Type: "userCount", Payload: len(clients)}
	}()

	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			break
		}

		if msg.Type == "emoji" {
			broadcast <- msg
		}
	}

	return nil
}

func handleNextSlide(c echo.Context) error {
	secret := c.FormValue("secret")
	if secret != config.Secret {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid secret"})
	}

	if currentSlide >= len(config.Survey) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Survey is already finished"})
	}

	currentSlide++
	if currentSlide >= len(config.Survey) {
		broadcast <- Message{Type: "finished", Payload: true}
		return c.NoContent(http.StatusSeeOther)
	}

	broadcast <- Message{Type: "newSlide", Payload: currentSlide}
	return c.NoContent(http.StatusOK)
}

func handleMessages() {
	for {
		msg := <-broadcast
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("error: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}
