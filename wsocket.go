package wslt

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

var (
	wSocket   *WSocket
	StdLogger = log.New(os.Stdout, "[WS]", log.LstdFlags)
)

func (w *WSocket) Handler(connector Connector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		webCtx := &WebContext{r: r, w: w}
		var err error
		if !webCtx.IsWebSocket() {
			webCtx.JSON(400, "不是一个WebSocket链接")
			return
		}
		var conn *websocket.Conn
		conn, err = upgradeOption.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if _, err = newConnection(connector, conn); err != nil {
			return
		}
		return
	}
}

//WSocket ...
type (
	IterationConnectionsFunc func(id int64, connector Connector)

	WSocket struct {
		mu sync.Mutex

		session *Session

		connections map[int64]Connector

		register chan Connector

		unregister chan Connector

		broadcast chan *BusinessMessage

		businessHandlers map[string]BusinessHandler
	}
)

func (w *WSocket) addConnections(sid int64, conn *Connection) {
	w.mu.Lock()
	if _, ok := w.connections[sid]; !ok {
		w.connections[sid] = conn.connector
	}
	w.mu.Unlock()
}

func (w *WSocket) removeConnection(sid int64) {
	w.mu.Lock()
	if _, ok := w.connections[sid]; !ok {
		delete(w.connections, sid)
	}
	w.mu.Unlock()
}

func (w *WSocket) Session() *Session {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.session
}

func (w *WSocket) runReadHandle(ctx *Context) {
	defer func() {
		if p := recover(); p != nil {
			StdLogger.Printf("run handler error:%v\n", p)
		}
	}()
	w.mu.Lock()
	if handler, has := w.businessHandlers[ctx.Message.StringType]; has {
		go handler(ctx)
	}
	w.mu.Unlock()
}

func (w *WSocket) ReadHandle(msgType string, handler BusinessHandler) {
	w.mu.Lock()
	if _, has := w.businessHandlers[msgType]; has {
		panic(fmt.Sprintf("BusinessHandler type:%s exists", msgType))
	}
	w.businessHandlers[msgType] = handler
	w.mu.Unlock()
}

func (w *WSocket) Len() (n int) {
	w.mu.Lock()
	n = len(w.connections)
	w.mu.Unlock()
	return
}

func (w *WSocket) IterationConnections(fn IterationConnectionsFunc) {
	w.mu.Lock()
	for key, value := range w.connections {
		fn(key, value)
	}
	w.mu.Unlock()
}

func newWSocket() *WSocket {
	ws := &WSocket{
		mu:               sync.Mutex{},
		session:          GlobalSession(),
		connections:      make(map[int64]Connector),
		register:         make(chan Connector, 1),
		unregister:       make(chan Connector, 1),
		broadcast:        make(chan *BusinessMessage, 256),
		businessHandlers: make(map[string]BusinessHandler),
	}
	go ws.run()
	return ws
}

func New() *WSocket {
	if wSocket == nil {
		wSocket = newWSocket()
	}
	return wSocket
}

func (w *WSocket) run() {
	for {
		select {
		case cn := <-w.register:
			cn.Connection().add()
		case cn := <-w.unregister:
			cn.Connection().close()
		case msg := <-w.broadcast:
			for _, cn := range w.connections {
				err := cn.Connection().sendBusinessMessage(msg)
				if err != nil {
					StdLogger.Printf("broadcast send message error:%s\n", err.Error())
				}
			}
		}
	}
}

func (w *WSocket) SendToAll(msgType string, data interface{}) (err error) {
	var msg *BusinessMessage
	if msg, err = NewBusinessMessage(msgType, data); err != nil {
		return
	}
	w.broadcast <- msg
	return
}
