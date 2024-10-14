package main

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var words = []string{
	"hello", "world", "baby", "golang", "programming",
	"survey", "random", "words", "generator", "app",
	"computer", "science", "data", "analysis", "cloud",
	"network", "security", "artificial", "intelligence", "machine",
	"learning", "algorithm", "database", "interface", "function",
}

func generateWords(count int) string {
	rand.Seed(time.Now().UnixNano())
	selectedWords := make([]string, count)
	for i := 0; i < count; i++ {
		selectedWords[i] = words[rand.Intn(len(words))]
	}
	return strings.Join(selectedWords, " ")
}

func submitWords(token string, words string) error {
	endpoint := fmt.Sprintf("http://localhost:8080/submit/%s", token)
	data := url.Values{}
	data.Set("answer", words)

	client := &http.Client{}
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 14_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.3 Mobile/15E148 Safari/604.1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/png,image/svg+xml,*/*;q=0.8")
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("Referer", fmt.Sprintf("http://localhost:8080/survey/%s", token))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	fmt.Printf("Response status: %s\n", resp.Status)

	return nil
}

func main() {
	token := "token" // Replace with the actual token
	wordCount := 3   // Number of words to generate and submit

	for i := 0; i < 2500000000; i++ { // Submit 5 times
		wordCount = rand.Intn(2) + 1
		wordCount = 1
		words := generateWords(wordCount)
		fmt.Printf("Submitting words: %s\n", words)

		err := submitWords(token, words)
		if err != nil {
			fmt.Printf("Error submitting words: %v\n", err)
		}

		time.Sleep(1 * time.Second) // Wait for 2 seconds between submissions
	}
}
