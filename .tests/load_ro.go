package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var (
	addr               = flag.String("addr", "192.168.1.240:8000", "http service address")
	maxConnections     = flag.Int("max", 1000, "maximum number of connections")
	rampUpTime         = flag.Duration("ramp", 1*time.Minute, "time to ramp up to max connections")
	testDuration       = flag.Duration("duration", 5*time.Minute, "total test duration")
	activeConnections  int32
	connectionAttempts int32
	maxConcurrent      int32
	messagesReceived   int32
	serverUserCount    int32
)

type UserCountMessage struct {
	Type    string `json:"type"`
	Payload int    `json:"payload"`
}

func main() {
	flag.Parse()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: *addr, Path: "/ws"}
	log.Printf("Connecting to %s", u.String())

	var wg sync.WaitGroup
	connectionChan := make(chan struct{})

	go func() {
		ticker := time.NewTicker(time.Duration(int64(*rampUpTime) / int64(*maxConnections)))
		defer ticker.Stop()

		for i := 0; i < *maxConnections; i++ {
			select {
			case <-ticker.C:
				wg.Add(1)
				go connect(u.String(), connectionChan, &wg)
				atomic.AddInt32(&connectionAttempts, 1)
			case <-connectionChan:
				return
			}
		}
	}()

	// Start a goroutine to periodically log the current state
	go func() {
		logTicker := time.NewTicker(5 * time.Second)
		defer logTicker.Stop()

		for {
			select {
			case <-logTicker.C:
				log.Printf("Current state - Active: %d, Attempts: %d, Max Concurrent: %d, Received: %d, Server userCount: %d",
					atomic.LoadInt32(&activeConnections),
					atomic.LoadInt32(&connectionAttempts),
					atomic.LoadInt32(&maxConcurrent),
					atomic.LoadInt32(&messagesReceived),
					atomic.LoadInt32(&serverUserCount))
			case <-connectionChan:
				return
			}
		}
	}()

	timeout := time.After(*testDuration)

	select {
	case <-timeout:
		log.Println("Test duration completed")
	case <-interrupt:
		log.Println("Interrupted")
	}

	log.Println("Closing all connections...")
	close(connectionChan)

	// Wait for all goroutines to finish with a timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All connections closed successfully")
	case <-time.After(10 * time.Second):
		log.Println("Timed out waiting for connections to close")
	}

	printResults()
}

func connect(urlStr string, done chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	c, _, err := websocket.DefaultDialer.Dial(urlStr, nil)
	if err != nil {
		log.Printf("Error connecting: %v (Active connections: %d, Attempts: %d)",
			err, atomic.LoadInt32(&activeConnections), atomic.LoadInt32(&connectionAttempts))
		return
	}
	defer c.Close()

	currentActive := atomic.AddInt32(&activeConnections, 1)
	defer atomic.AddInt32(&activeConnections, -1)

	// Update max concurrent connections
	for {
		current := atomic.LoadInt32(&maxConcurrent)
		if currentActive <= current {
			break
		}
		if atomic.CompareAndSwapInt32(&maxConcurrent, current, currentActive) {
			break
		}
	}

	// Set up a separate channel for reading errors
	readErr := make(chan error, 1)

	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}

			atomic.AddInt32(&messagesReceived, 1)

			var msg UserCountMessage
			if err := json.Unmarshal(message, &msg); err == nil && msg.Type == "userCount" {
				atomic.StoreInt32(&serverUserCount, int32(msg.Payload))
			}
		}
	}()

	select {
	case <-done:
		// Attempt to close the WebSocket connection gracefully
		err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			log.Printf("Error during closing websocket: %v", err)
		}
		// Wait for the server to close the connection
		select {
		case <-readErr:
		case <-time.After(time.Second):
		}
	case err := <-readErr:
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			log.Printf("Error reading message: %v", err)
		}
	}
}

func printResults() {
	fmt.Printf("Test completed\n")
	fmt.Printf("Max connections attempted: %d\n", *maxConnections)
	fmt.Printf("Total connection attempts: %d\n", atomic.LoadInt32(&connectionAttempts))
	fmt.Printf("Max concurrent connections: %d\n", atomic.LoadInt32(&maxConcurrent))
	fmt.Printf("Active connections at end: %d\n", atomic.LoadInt32(&activeConnections))
	fmt.Printf("Messages received: %d\n", atomic.LoadInt32(&messagesReceived))
	fmt.Printf("Final server userCount: %d\n", atomic.LoadInt32(&serverUserCount))
}
