package wslt

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	_        = iota             // ignore first value by assigning to blank identifier
	KB int64 = 1 << (10 * iota) // 1 << (10*1)
	MB                          // 1 << (10*2)
	GB                          // 1 << (10*3)
	TB                          // 1 << (10*4)
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 10 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = KB * 20
)

var (
	upgradeOption = websocket.Upgrader{
		HandshakeTimeout: time.Second * 3,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
			webCtx := &WebContext{r: r, w: w}
			webCtx.JSON(status, reason.Error())
		},
		CheckOrigin: func(r *http.Request) (ok bool) {
			ok = true
			return
		},
	}
)

var (
	//ErrUnauthorized auth err
	ErrUnauthorized = errors.New(http.StatusText(http.StatusUnauthorized))
)
