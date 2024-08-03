package main

import (
	"log"
	"net/http"

	"github.com/atcharles/wslt"
)

//go:generate go build -race -trimpath -v -ldflags "-s -w" -o client .

func main() {
	client, err := wslt.NewClient("ws://127.0.0.1:8080/ws", http.Header{
		"token": []string{"123456"},
	})
	defer client.Close()
	if err != nil {
		log.Fatalf("NewClient error: %v", err)
	}
	if e := client.SendMessage("test", "hello"); e != nil {
		log.Fatalf("SendMessage error: %v", e)
	}

	wslt.WaitSignal()
}
