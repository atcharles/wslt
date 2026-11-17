package wslt

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var StdLogger = log.New(os.Stdout, "[WS-LT] ", log.LstdFlags)

type IterationConnectionsFunc func(sessionID string, connection *Connection) bool

type WSocket struct {
	mu *sync.RWMutex

	done                chan struct{}
	closeOnce           sync.Once
	broadcastQueueSize  int
	connectionQueueSize int
	broadcastTimeout    time.Duration

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
	select {
	case w.iterationChan <- fn:
	case <-w.done:
	}
}

// SendToAll queues a broadcast, waiting at most the configured timeout.
func (w *WSocket) SendToAll(msgType string, data interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.broadcastTimeout)
	defer cancel()
	return w.SendToAllContext(ctx, msgType, data)
}

// SendToAllContext waits for queue space until ctx is canceled or the hub closes.
// Success means queued, not delivered; slow peers are disconnected during fanout.
func (w *WSocket) SendToAllContext(ctx context.Context, msgType string, data interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-w.done:
		return errors.New("websocket hub is closed")
	default:
	}
	msg, err := CreateWsMessage(msgType, data)
	if err != nil {
		return err
	}
	select {
	case w.broadcast <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return errors.New("websocket hub is closed")
	}
}

// Close stops the hub and closes its connections. It is safe to call repeatedly.
func (w *WSocket) Close() {
	w.closeOnce.Do(func() { close(w.done) })
	for _, conn := range w.connectionSnapshot() {
		conn.close()
	}
}

// ConnectionCount returns the current number of registered connections.
func (w *WSocket) ConnectionCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.connections)
}

func (w *WSocket) connectionSnapshot() map[string]*Connection {
	w.mu.RLock()
	defer w.mu.RUnlock()
	connections := make(map[string]*Connection, len(w.connections))
	for sid, conn := range w.connections {
		connections[sid] = conn
	}
	return connections
}

func (w *WSocket) removeConnection(conn *Connection) {
	w.mu.Lock()
	delete(w.connections, conn.sessionID)
	w.mu.Unlock()
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
		case <-w.done:
			return
		case conn := <-w.register:
			w.mu.Lock()
			if !conn.IsClosed() {
				select {
				case <-w.done:
				default:
					w.connections[conn.sessionID] = conn
				}
			}
			w.mu.Unlock()
			select {
			case <-w.done:
				conn.close()
			default:
			}
		case conn := <-w.unregister:
			w.removeConnection(conn)
		case msg := <-w.broadcast:
			for _, conn := range w.connectionSnapshot() {
				if e := conn.trySendWsMessage(msg); e != nil {
					conn.close()
					StdLogger.Printf("broadcast error :%v; sid:%s", e, conn.sessionID)
				}
			}
		case fn := <-w.iterationChan:
			for key, value := range w.connectionSnapshot() {
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
		mu:                  &sync.RWMutex{},
		connections:         make(map[string]*Connection),
		register:            make(chan *Connection),
		unregister:          make(chan *Connection),
		done:                make(chan struct{}),
		broadcastQueueSize:  defaultBroadcastQueueSize,
		connectionQueueSize: defaultConnectionQueueSize,
		broadcastTimeout:    defaultBroadcastTimeout,
		businessHandlers:    make(map[string]BusinessHandler),
		iterationChan:       make(chan IterationConnectionsFunc, 1),
	}
	for _, opt := range opts {
		opt(ws)
	}
	ws.broadcast = make(chan *WsMessage, ws.broadcastQueueSize)
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
