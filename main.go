package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

var (
	config        Config
	currentSlide  int32 = -1
	answers       sync.Map
	clients       sync.Map
	broadcast     = make(chan Message, 100)
	upgrader      = websocket.Upgrader{}
	userResponses sync.Map
	clientCount   int32 = 0
)

const (
	userIDCookieName = "opensurvey_cookie"
	cookieMaxAge     = 2 * 60 * 60 // 2 hours in seconds
)

func main() {
	loadConfig("config.yaml")

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.Static("/static", "static")

	t := template.Must(template.ParseGlob("views/*.html"))
	template.Must(t.ParseGlob("views/_layout/*.html"))
	template.Must(t.ParseGlob("views/components/*.html"))
	e.Renderer = &TemplateRenderer{templates: t}

	e.GET("/", handleIndex)
	e.POST("/", handleToken)
	e.GET("/survey/:token", handleSurvey)
	e.POST("/submit/:token", handleSubmit)
	e.GET("/results/:token", handleResults)
	e.GET("/completed/:token", handleCompleted)
	e.GET("/presenter", handlePresenter)
	e.GET("/ws", handleWebSocket)
	e.GET("/nextSlide", handleNextSlide)

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
	data, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}
}

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
		Secure:   c.Request().TLS != nil,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	}
	c.SetCookie(cookie)

	return userID, nil
}

func handleIndex(c echo.Context) error {
	return c.Render(http.StatusOK, "index.html", nil)
}

func handleToken(c echo.Context) error {
	token := c.FormValue("token")

	if token == "" {
		return c.String(http.StatusBadRequest, "Token is required")
	}

	if token == config.Secret {

		// Create a new cookie with the token
		cookie := new(http.Cookie)
		cookie.Name = userIDCookieName
		cookie.Value = token
		cookie.HttpOnly = true                 // Makes the cookie inaccessible to JavaScript
		cookie.Secure = c.Request().TLS != nil // Only send over HTTPS
		cookie.SameSite = http.SameSiteStrictMode
		cookie.Path = "/"

		// Set the cookie
		c.SetCookie(cookie)

		// Redirect to the presenter page
		return c.Redirect(http.StatusFound, "/presenter")
	} else {
		return c.Redirect(http.StatusFound, "/survey/"+token)
	}
}

func handleCompleted(c echo.Context) error {
	token := c.Param("token")
	if token != config.Token {
		return c.String(http.StatusUnauthorized, "Invalid token")
	}

	if int(currentSlide) >= len(config.Survey) {
		return c.Render(http.StatusOK, "completed.html", nil)
	}

	if currentSlide == 0 {
		return c.Render(http.StatusOK, "waiting.html", map[string]interface{}{
			"UserCount":  atomic.LoadInt32(&clientCount),
			"SurveyName": config.Name,
		})
	}

	clearUserIDCookie(c)

	return c.Redirect(http.StatusSeeOther, "/survey/"+token)
}

func handlePresenter(c echo.Context) error {
	// Retrieve the token from the cookie
	cookie, err := c.Cookie(userIDCookieName)
	if err != nil || cookie.Value != config.Secret {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	return c.Render(http.StatusOK, "presenter.html", map[string]interface{}{
		"Token": config.Token,
	})
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

	if int(currentSlide) >= len(config.Survey) {
		return c.Render(http.StatusOK, "completed.html", nil)
	}

	if currentSlide == -1 {
		return c.Render(http.StatusOK, "waiting.html", map[string]interface{}{
			"UserCount":  atomic.LoadInt32(&clientCount),
			"SurveyName": config.Name,
		})
	}

	if hasUserAnswered(token, int(currentSlide), userID) {
		return c.Redirect(http.StatusSeeOther, fmt.Sprintf("/results/%s", token))
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

	if hasUserAnswered(token, int(currentSlide), userID) {
		return c.Redirect(http.StatusSeeOther, fmt.Sprintf("/results/%s", token))
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
			body, err := io.ReadAll(c.Request().Body)
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

	storeAnswers(token, int(currentSlide), userID, selectedAnswers)

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
	userKey := fmt.Sprintf("%s:%d:%s", token, slide, userID)
	userResponses.Store(userKey, true)
}

func getAnswers(key string) []string {
	if existingAns, ok := answers.Load(key); ok {
		return existingAns.([]string)
	}
	return []string{}
}

func hasUserAnswered(token string, slide int, userID string) bool {
	userKey := fmt.Sprintf("%s:%d:%s", token, slide, userID)
	_, answered := userResponses.Load(userKey)
	return answered
}

func getResults(token string) map[string]int {
	results := make(map[string]int)
	key := fmt.Sprintf("%s:%d", token, currentSlide)
	for _, answer := range getAnswers(key) {
		results[answer]++
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
	hasAnswered := hasUserAnswered(token, int(currentSlide), userID)

	// Add all possible answers to the results
	currentSlide := config.Survey[currentSlide]
	for _, answer := range currentSlide.Answers {
		if _, exists := results[answer]; !exists {
			results[answer] = 0
		}
	}

	return c.Render(http.StatusOK, "results.html", map[string]interface{}{
		"Slide":       currentSlide,
		"Results":     results,
		"HasAnswered": hasAnswered,
	})
}

func handleWebSocket(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	newCount := atomic.AddInt32(&clientCount, 1)
	clients.Store(ws, true)
	broadcast <- Message{Type: "userCount", Payload: newCount}
	defer func() {
		clients.Delete(ws)
		ws.Close()
		newCount := atomic.AddInt32(&clientCount, -1)
		broadcast <- Message{Type: "userCount", Payload: newCount}
	}()
	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			break
		}
		if msg.Type == "emoji" || msg.Type == "emojiPopped" {
			broadcast <- msg
		}
	}
	return nil
}

func handleNextSlide(c echo.Context) error {
	// Check for authentication via cookie or header
	authenticated := false

	cookie, err := c.Cookie(userIDCookieName)
	if err == nil || cookie.Value == config.Secret {
		authenticated = true
	}

	secret := c.Request().Header.Get("x-token")
	if secret == config.Secret {
		authenticated = true
	}

	if !authenticated {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid secret"})
	}

	if int(currentSlide) >= len(config.Survey) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Survey is already finished"})
	}

	currentSlide++
	if int(currentSlide) >= len(config.Survey) {
		broadcast <- Message{Type: "finished", Payload: true}
		return c.NoContent(http.StatusSeeOther)
	}

	broadcast <- Message{Type: "newSlide", Payload: currentSlide}
	return c.NoContent(http.StatusOK)
}

func handleMessages() {
	for msg := range broadcast {
		clients.Range(func(key, value interface{}) bool {
			client := key.(*websocket.Conn)
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("error: %v", err)
				client.Close()
				clients.Delete(client)
			}
			return true
		})
	}
}
