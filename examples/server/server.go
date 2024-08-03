package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/atcharles/wslt"
)

//go:generate go build -race -trimpath -v -ldflags "-s -w" -o server .

func main() {
	ws := wslt.New()
	ws.ReadHandle("test", func(c *wslt.Context) {
		log.Printf("test: %s", c.Message.Data)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.Handler(&User{ID: 188}))

	log.Println("Server start")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

type User struct {
	ID int64

	conn *wslt.Connection
}

func (u *User) AuthorizationChecker() func(token string) bool {
	return func(token string) bool {
		return token == "123456"
	}
}

func (u *User) CheckAuth(string, *websocket.Conn) error {
	return nil
}

func (u *User) Connection() *wslt.Connection {
	return u.conn
}

func (u *User) GetID() int64 {
	return u.ID
}

func (u *User) NewInstance() wslt.ItfConnector {
	return u
}

func (u *User) SetConnection(conn *wslt.Connection) {
	u.conn = conn
}
