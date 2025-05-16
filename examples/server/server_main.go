package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/atcharles/wslt"
)

//go:generate rm -rf ./logs
//go:generate sh -c "CGO_ENABLED=1 go build -race -v -trimpath -ldflags \"-s -w\" -o server ."
//go:generate ./server
func main() {
	ws := wslt.NewWSocket(
		wslt.WithBusinessHandlers(wslt.BusinessHandlers{
			"test": func(c *wslt.Context) {
				log.Printf("test: %s", c.Message.Data)
			},
			"rpcTest": func(c *wslt.Context) {
				log.Printf("rpcTest: %s", c.Message.Data)
				callMsg, err := c.BindCallRequest()
				if err != nil {
					log.Printf("BindCallRequest error: %v", err)
					return
				}
				if e := c.SendCallBackMsg(callMsg, "this is rpcTest result:"+callMsg.RequestID); e != nil {
					log.Printf("SendCallBackMsg error: %v", e)
					return
				}
			},
		}),

		/*wslt.WithAfterConnectFunc(func(c *wslt.Connection) {
			log.Printf(
				"AfterConnect: sessionID:%s, userID:%s",
				c.SessionID(),
				c.UserID(),
			)
		}),*/

		wslt.WithAuthChecker(func(auth string) (userID string, err error) {
			const userToken = "123456"
			if auth != userToken {
				err = errors.New("invalid token")
				return
			}
			return "1", nil
		}),
	)

	go func() {
		for {
			if err := ws.SendToAll("messageClient", fmt.Sprintf("from server, time now %s", time.Now())); err != nil {
				log.Printf("SendMessage error: %v", err)
			}
			time.Sleep(time.Millisecond * 1000)
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/ws", ws)

	log.Println("Server start")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

/*type User struct {
	ID int64

	conn *wslt.Connection
}*/
