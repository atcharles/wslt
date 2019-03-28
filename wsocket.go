package wslt

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var (
	wSocket   *WSocket
	StdLogger = log.New(os.Stdout, "[WSlt] ", log.LstdFlags)
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
		StdLogger.Printf("接受连接:%s\n", httpRequestAddr(r))
		return
	}
}

func httpRequestAddr(r *http.Request) string {
	clientIP := r.Header.Get("X-Forwarded-For")
	clientIP = strings.TrimSpace(strings.Split(clientIP, ",")[0])
	if clientIP == "" {
		clientIP = strings.TrimSpace(r.Header.Get("X-Real-Ip"))
	}
	if clientIP != "" {
		return clientIP
	}

	if ip, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return ip
	}

	return ""
}

//WSocket ...
type (
	IterationConnectionsFunc func(id int64, connection *Connection) bool

	WSocket struct {
		mu *sync.Mutex

		session *Session
		//sid=>connector
		connections map[int64]*Connection
		//add sid
		register chan *Connection
		//remove sid
		unregister chan *Connection
		//send broadcast to all connection
		broadcast chan *WsMessage
		//handle read data
		businessHandlers map[string]BusinessHandler

		iterationChan chan IterationConnectionsFunc
	}
)

func (w *WSocket) runReadHandle(ctx *Context) {
	defer func() {
		if p := recover(); p != nil {
			StdLogger.Printf("run handler error:%v\n", p)
		}
	}()
	if handler, has := w.businessHandlers[ctx.Message.StringType]; has {
		go handler(ctx)
	}
}

func (w *WSocket) ReadHandle(msgType string, handler BusinessHandler) {
	if _, has := w.businessHandlers[msgType]; has {
		panic(fmt.Sprintf("BusinessHandler type:%s exists", msgType))
	}
	w.businessHandlers[msgType] = handler
}

func (w *WSocket) IterationConnections(fn IterationConnectionsFunc) {
	w.iterationChan <- fn
}

func newWSocket() *WSocket {
	ws := &WSocket{
		mu:               &sync.Mutex{},
		session:          GlobalSession(),
		connections:      make(map[int64]*Connection),
		register:         make(chan *Connection, 1),
		unregister:       make(chan *Connection, 1),
		broadcast:        make(chan *WsMessage, 256),
		businessHandlers: make(map[string]BusinessHandler),
		iterationChan:    make(chan IterationConnectionsFunc, 1),
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
		case conn := <-w.register:
			StdLogger.Printf("register connectorID:%d;sid:%d;Address:%s\n",
				conn.connector.GetID(), conn.sessionID, conn.conn.RemoteAddr().String())
			w.connections[conn.sessionID] = conn
		case conn := <-w.unregister:
			if _, ok := w.connections[conn.sessionID]; ok {
				StdLogger.Printf("unregister connectorID:%d;sid:%d;Address:%s\n",
					conn.connector.GetID(), conn.sessionID, conn.conn.RemoteAddr().String())
				delete(w.connections, conn.sessionID)
			}
		case msg := <-w.broadcast:
			for _, conn := range w.connections {
				select {
				case conn.sent <- msg:
				case <-conn.closeChan:
					continue
				}
			}
		case fn := <-w.iterationChan:
			for key, value := range w.connections {
				if !fn(key, value) {
					break
				}
			}
		}
	}
}

func (w *WSocket) SendToAll(msgType string, data interface{}) (err error) {
	var msg *WsMessage
	if msg, err = CreateWsMessage(msgType, data); err != nil {
		return
	}
	w.broadcast <- msg
	return
}
