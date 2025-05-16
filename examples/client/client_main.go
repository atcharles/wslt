package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/atcharles/wslt"
	"github.com/atcharles/wslt/ws_rpc"
)

//go:generate rm -rf ./logs
//go:generate sh -c "CGO_ENABLED=1 go build -race -v -trimpath -ldflags \"-s -w\" -o client ."
//go:generate ./client
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

		go func() {
			for {
				if e := client.SendMessage("test", fmt.Sprintf("time now %s", time.Now())); e != nil {
					log.Printf("SendMessage error: %v", e)
				}
				time.Sleep(time.Millisecond * 1000)
			}
		}()

		go func() {
			for {
				rst, err := ws_rpc.NewCallMessage(client).Call("rpcTest", ws_rpc.Map{"key": "value"})
				if err != nil {
					log.Printf("Call error: %v", err)
					continue
				}
				log.Printf("Call result: %s", string(rst))
				time.Sleep(time.Millisecond * 1000)
			}
		}()
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
	client.AddBusinessHandler("messageClient", func(c *wslt.ClientContext) {
		log.Printf("messageClient: %s\n", c.Message.Data)
	})
	if err := client.Dial(); err != nil {
		log.Fatalf("Dial error: %v", err)
	}
	wslt.WaitSignal()
}
