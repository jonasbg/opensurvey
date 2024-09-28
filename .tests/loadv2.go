package main

import (
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
	addr              = flag.String("addr", "localhost:8000", "http service address")
	maxConnections    = flag.Int("max", 1000, "maximum number of connections")
	rampUpTime        = flag.Duration("ramp", 1*time.Minute, "time to ramp up to max connections")
	testDuration      = flag.Duration("duration", 5*time.Minute, "total test duration")
	activeConnections int32
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
			case <-interrupt:
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
		log.Printf("Error connecting: %v", err)
		return
	}
	defer c.Close()

	atomic.AddInt32(&activeConnections, 1)
	defer atomic.AddInt32(&activeConnections, -1)

	for {
		select {
		case <-done:
			return
		default:
			_, _, err := c.ReadMessage()
			if err != nil {
				log.Printf("Error reading message: %v", err)
				return
			}
		}
	}
}

func printResults() {
	fmt.Printf("Test completed\n")
	fmt.Printf("Max connections: %d\n", *maxConnections)
	fmt.Printf("Active connections at end: %d\n", atomic.LoadInt32(&activeConnections))
}
