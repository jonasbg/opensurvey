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
	messageInterval    = flag.Duration("msginterval", 5*time.Second, "interval between messages")
	activeConnections  int32
	connectionAttempts int32
	maxConcurrent      int32
	messagesSent       int32
	messagesReceived   int32
	messagesFailed     int32
	serverUserCount    int32
)

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
			case <-interrupt:
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
				log.Printf("Current state - Active: %d, Attempts: %d, Max Concurrent: %d, Sent: %d, Received: %d, Failed: %d, Server userCount: %d",
					atomic.LoadInt32(&activeConnections),
					atomic.LoadInt32(&connectionAttempts),
					atomic.LoadInt32(&maxConcurrent),
					atomic.LoadInt32(&messagesSent),
					atomic.LoadInt32(&messagesReceived),
					atomic.LoadInt32(&messagesFailed),
					atomic.LoadInt32(&serverUserCount))
			case <-connectionChan:
				return
			}
		}
	}()

	timeout := time.After(*testDuration)

	for {
		select {
		case <-timeout:
			log.Println("Test duration completed")
			close(connectionChan)
			wg.Wait()
			printResults()
			return
		case <-interrupt:
			log.Println("Interrupted")
			close(connectionChan)
			wg.Wait()
			printResults()
			return
		}
	}
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

	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Printf("Error reading message: %v", err)
				return
			}

			var msg struct {
				Type    string `json:"type"`
				Payload struct {
					UserCount int `json:"userCount"`
				} `json:"payload"`
			}

			if err := json.Unmarshal(message, &msg); err == nil && msg.Type == "userCount" {
				atomic.StoreInt32(&serverUserCount, int32(msg.Payload.UserCount))
			}

			atomic.AddInt32(&messagesReceived, 1)
		}
	}()

	messageTicker := time.NewTicker(*messageInterval)
	defer messageTicker.Stop()

	for {
		select {
		case <-done:
			return
		case <-messageTicker.C:
			// err := c.WriteMessage(websocket.TextMessage, []byte("Test message"))
			// if err != nil {
			// 	atomic.AddInt32(&messagesFailed, 1)
			// 	log.Printf("Error sending message: %v (Active connections: %d)", err, atomic.LoadInt32(&activeConnections))
			// 	return
			// }
			// atomic.AddInt32(&messagesSent, 1)

			_, _, err = c.ReadMessage()
			if err != nil {
				atomic.AddInt32(&messagesFailed, 1)
				log.Printf("Error reading message: %v (Active connections: %d)", err, atomic.LoadInt32(&activeConnections))
				return
			}
			atomic.AddInt32(&messagesReceived, 1)
		}
	}
}

func printResults() {
	fmt.Printf("Test completed\n")
	fmt.Printf("Max connections attempted: %d\n", *maxConnections)
	fmt.Printf("Total connection attempts: %d\n", atomic.LoadInt32(&connectionAttempts))
	fmt.Printf("Max concurrent connections: %d\n", atomic.LoadInt32(&maxConcurrent))
	fmt.Printf("Active connections at end: %d\n", atomic.LoadInt32(&activeConnections))
	fmt.Printf("Messages sent: %d\n", atomic.LoadInt32(&messagesSent))
	fmt.Printf("Messages received: %d\n", atomic.LoadInt32(&messagesReceived))
	fmt.Printf("Messages failed: %d\n", atomic.LoadInt32(&messagesFailed))
}
