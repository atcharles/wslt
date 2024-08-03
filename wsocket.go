package wslt

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

var (
	wSocket   *WSocket
	StdLogger = log.New(os.Stdout, "[WS-LT] ", log.LstdFlags)
)

// WSocket ...
type (
	IterationConnectionsFunc func(sessionID string, connection *Connection) bool

	WSocket struct {
		mu *sync.RWMutex

		session *Session

		connections map[string]*Connection

		register chan *Connection

		unregister chan *Connection

		broadcast chan *WsMessage

		businessHandlers map[string]BusinessHandler

		iterationChan chan IterationConnectionsFunc
	}
)

func (w *WSocket) Handler(connector ItfConnector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		webCtx := &WebContext{r: r, w: w}
		if !webCtx.IsWebSocket() {
			webCtx.JSON(400)
			return
		}
		connector = connector.NewInstance()
		authorizationChecker := connector.AuthorizationChecker()
		if authorizationChecker != nil {
			token := getRequestToken(r)
			if !authorizationChecker(token) {
				webCtx.JSON(401)
				return
			}
		}
		conn, err := upgradeOption.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		con := newConnection(connector, conn)
		StdLogger.Printf(
			"Accept connection: %s,ConnectorID:%d,SessionID:%s",
			httpRequestAddr(r),
			connector.GetID(),
			con.sessionID,
		)
	}
}

func (w *WSocket) IterationConnections(fn IterationConnectionsFunc) {
	w.iterationChan <- fn
}

func (w *WSocket) ReadHandle(msgType string, handler BusinessHandler) {
	if _, has := w.getRHandlers()[msgType]; has {
		panic(fmt.Sprintf("BusinessHandler type:%s exists", msgType))
	}
	w.mu.Lock()
	w.businessHandlers[msgType] = handler
	w.mu.Unlock()
}

func (w *WSocket) SendToAll(msgType string, data interface{}) (err error) {
	var msg *WsMessage
	if msg, err = CreateWsMessage(msgType, data); err != nil {
		return
	}
	w.broadcast <- msg
	return
}

// getRHandlers ...
func (w *WSocket) getRHandlers() map[string]BusinessHandler {
	w.mu.RLock()
	hs := make(map[string]BusinessHandler, len(w.businessHandlers))
	for k, v := range w.businessHandlers {
		hs[k] = v
	}
	w.mu.RUnlock()
	return hs
}

func (w *WSocket) run() {
	for {
		select {
		case conn := <-w.register:
			StdLogger.Printf(
				"register connectorID:%d;sid:%s;Address:%s",
				conn.connector.GetID(),
				conn.sessionID,
				conn.conn.RemoteAddr().String(),
			)
			w.connections[conn.sessionID] = conn
		case conn := <-w.unregister:
			if _, ok := w.connections[conn.sessionID]; ok {
				StdLogger.Printf(
					"unregister connectorID:%d;sid:%s;Address:%s",
					conn.connector.GetID(),
					conn.sessionID,
					conn.conn.RemoteAddr().String(),
				)
				delete(w.connections, conn.sessionID)
			}
		case msg := <-w.broadcast:
			for _, conn := range w.connections {
				if e := conn.SendWsMessage(msg); e != nil {
					StdLogger.Printf(
						"broadcast error :%v; connectorID:%d;sid:%s",
						e,
						conn.connector.GetID(),
						conn.sessionID,
					)
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

func (w *WSocket) runReadHandle(ctx *Context) {
	if handler, has := w.getRHandlers()[ctx.Message.Type]; has {
		handler(ctx)
	}
}

func New() *WSocket {
	if wSocket == nil {
		wSocket = newWSocket()
	}
	return wSocket
}

func getRequestToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	if token == "" {
		token = r.Header.Get("token")
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		token = r.FormValue("token")
	}
	return token
}

func httpRequestAddr(r *http.Request) string {
	clientIP := r.Header.Get("X-Forwarded-For")

	ips := strings.Split(clientIP, ",")
	if len(ips) > 0 {
		clientIP = ips[0]
	}
	if clientIP != "" {
		return clientIP
	}

	clientIP = strings.TrimSpace(r.Header.Get("X-Real-Ip"))
	if clientIP != "" {
		return clientIP
	}

	if ip, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return ip
	}
	return ""
}

func newWSocket() *WSocket {
	ws := &WSocket{
		mu:               &sync.RWMutex{},
		session:          GlobalSession(),
		connections:      make(map[string]*Connection),
		register:         make(chan *Connection),
		unregister:       make(chan *Connection),
		broadcast:        make(chan *WsMessage, 10*MB),
		businessHandlers: make(map[string]BusinessHandler),
		iterationChan:    make(chan IterationConnectionsFunc, 1),
	}
	go ws.run()
	return ws
}
