package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/atcharles/wslt"
)

//go:generate go build -race -trimpath -v -ldflags "-s -w" -o client .
func main() {
	fn1onConnected := func(client *wslt.Client) {
		if e := client.SendMessage("test", "hello"); e != nil {
			log.Fatalf("SendMessage error: %v", e)
		}

		for i := 0; i < 3; i++ {
			msg := fmt.Sprintf("hello %d", i)
			if e := client.SendMessage("test", msg); e != nil {
				log.Fatalf("SendMessage error: %v", e)
			}
			time.Sleep(time.Millisecond * 1000)
		}
	}
	client := wslt.NewClient(&wslt.ClientConfig{
		Url: "ws://127.0.0.1:8080/ws",
		RequestHeader: http.Header{
			"token": []string{"123456"},
		},
		OnConnected: func(c *wslt.Client) {
			log.Println("Connected")
			fn1onConnected(c)
		},
		OnClose: func(*wslt.Client) {
			log.Println("Closed")
		},
		OnReconnectFail: func(*wslt.Client) {
			log.Println("ReConnectFail")
		},
		OnReconnected: func(c *wslt.Client) {
			log.Println("ReConnected")
		},
	})
	defer client.Close()
	if err := client.Dial(); err != nil {
		log.Fatalf("Dial error: %v", err)
	}
	wslt.WaitSignal()
}
