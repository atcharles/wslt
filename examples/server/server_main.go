package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/atcharles/wslt"
)

//go:generate go build -race -trimpath -v -ldflags "-s -w" -o server .
func main() {
	ws := wslt.NewWSocket(
		wslt.WithBusinessHandlers(wslt.BusinessHandlers{
			"test": func(c *wslt.Context) {
				log.Printf("test: %s", c.Message.Data)
			},
		}),

		wslt.WithAfterConnectFunc(func(c *wslt.Connection) {
			log.Printf(
				"AfterConnect: sessionID:%s, userID:%s",
				c.SessionID(),
				c.UserID(),
			)
		}),

		wslt.WithAuthChecker(func(auth string) (userID string, err error) {
			const userToken = "123456"
			if auth != userToken {
				err = errors.New("invalid token")
				return
			}
			return "1", nil
		}),
	)

	mux := http.NewServeMux()
	mux.Handle("/ws", ws)

	log.Println("Server start")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

type User struct {
	ID int64

	conn *wslt.Connection
}
