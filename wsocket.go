package wslt

import (
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

var StdLogger = log.New(os.Stdout, "[WS-LT] ", log.LstdFlags)

type IterationConnectionsFunc func(sessionID string, connection *Connection) bool

type WSocket struct {
	mu *sync.RWMutex

	connections map[string]*Connection

	register chan *Connection

	unregister chan *Connection

	broadcast chan *WsMessage

	businessHandlers map[string]BusinessHandler

	iterationChan chan IterationConnectionsFunc

	authChecker AuthChecker

	afterConnectFunc AfterConnectFunc
}

func (w *WSocket) AddBusinessHandler(msgType string, handler BusinessHandler) {
	if _, has := w.getRHandlers()[msgType]; has {
		//panic(fmt.Sprintf("BusinessHandler type:%s exists", msgType))
		return
	}
	w.mu.Lock()
	w.businessHandlers[msgType] = handler
	w.mu.Unlock()
}

func (w *WSocket) IterationConnections(fn IterationConnectionsFunc) {
	w.iterationChan <- fn
}

func (w *WSocket) SendToAll(msgType string, data interface{}) (err error) {
	var msg *WsMessage
	if msg, err = CreateWsMessage(msgType, data); err != nil {
		return
	}
	w.broadcast <- msg
	return
}

// SendToSession ...
func (w *WSocket) SendToSession(sessionID, msgType string, data interface{}) (err error) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	var msg *WsMessage
	if msg, err = CreateWsMessage(msgType, data); err != nil {
		return
	}
	w.IterationConnections(func(sid string, connection *Connection) bool {
		if sid == sessionID {
			if e := connection.SendWsMessage(msg); e != nil {
				StdLogger.Printf("SendToSession error :%v; sid:%s", e, connection.sessionID)
			}
			return false
		}
		return true
	})
	return
}

// SendToUser ...
func (w *WSocket) SendToUser(userID, msgType string, data interface{}) (err error) {
	if strings.TrimSpace(userID) == "" {
		return
	}
	var msg *WsMessage
	if msg, err = CreateWsMessage(msgType, data); err != nil {
		return
	}
	w.IterationConnections(func(_ string, connection *Connection) bool {
		if connection.userID == userID {
			if e := connection.SendWsMessage(msg); e != nil {
				StdLogger.Printf("SendToUser error :%v; sid:%s", e, connection.sessionID)
			}
		}
		return true
	})
	return
}

func (w *WSocket) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	webCtx := &WebContext{r: request, w: writer}
	if !webCtx.IsWebSocket() {
		webCtx.JSON(400)
		return
	}
	var userID string
	if w.authChecker != nil {
		token := getRequestToken(request)
		id, err := w.authChecker(token)
		if err != nil {
			webCtx.JSON(401, err)
			return
		}
		userID = id
	}
	conn, err := upgradeOption.Upgrade(writer, request, writer.Header())
	if err != nil {
		return
	}
	con := newConnection(conn, w)
	con.userID = userID
	con.ip = httpRequestAddr(request)
	if w.afterConnectFunc != nil {
		w.afterConnectFunc(con)
	}
	con.register()
	//StdLogger.Printf("Accept connection: %s, SessionID:%s", con.ip, con.sessionID)
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
			StdLogger.Printf("register sid:%s; Address:%s", conn.sessionID, conn.ip)
			w.connections[conn.sessionID] = conn
		case conn := <-w.unregister:
			if _, ok := w.connections[conn.sessionID]; ok {
				StdLogger.Printf("unregister sid:%s; Address:%s", conn.sessionID, conn.ip)
				delete(w.connections, conn.sessionID)
			}
		case msg := <-w.broadcast:
			for _, conn := range w.connections {
				if e := conn.SendWsMessage(msg); e != nil {
					StdLogger.Printf("broadcast error :%v; sid:%s", e, conn.sessionID)
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

func NewWSocket(opts ...WsOptionFunc) *WSocket {
	ws := &WSocket{
		mu:               &sync.RWMutex{},
		connections:      make(map[string]*Connection),
		register:         make(chan *Connection),
		unregister:       make(chan *Connection),
		broadcast:        make(chan *WsMessage, 10*MB),
		businessHandlers: make(map[string]BusinessHandler),
		iterationChan:    make(chan IterationConnectionsFunc, 1),
	}
	for _, opt := range opts {
		opt(ws)
	}
	go ws.run()
	return ws
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
	lookupFromHeaders := []string{
		"CF-Connecting-IP",
		"X-Forwarded-For",
		"X-Real-IP",
	}
	var clientIP string

	for _, header := range lookupFromHeaders {
		str := strings.TrimSpace(r.Header.Get(header))
		sl := strings.Split(str, ",")
		if len(sl) > 0 {
			clientIP = strings.TrimSpace(sl[0])
			if clientIP != "" {
				return clientIP
			}
		}
	}

	if ip, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return ip
	}
	return ""
}
