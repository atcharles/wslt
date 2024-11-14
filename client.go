package wslt

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/atcharles/wslt/ws_rpc"

	"github.com/gorilla/websocket"
)

type Client struct {
	mu sync.RWMutex

	conn *websocket.Conn

	closed bool

	closeChan chan byte

	sent chan *WsMessage

	businessHandlers map[string]ClientBusinessHandler

	rpcMessages map[string]ws_rpc.CallHandler

	config *ClientConfig

	reconnectTimes uint32
}

func (c *Client) AddBusinessHandler(typeStr string, handler ClientBusinessHandler) {
	c.mu.Lock()
	c.businessHandlers[typeStr] = handler
	c.mu.Unlock()
}

func (c *Client) AddCallHandler(rid string, handler ws_rpc.CallHandler) {
	c.mu.Lock()
	c.rpcMessages[rid] = handler
	c.mu.Unlock()
}

func (c *Client) Close() {
	c.close()
}

func (c *Client) CloseChan() <-chan byte {
	return c.closeChan
}

func (c *Client) Dial() (err error) {
	if err = c.connect(); err != nil {
		return
	}
	if c.config.OnConnected != nil {
		c.config.OnConnected(c)
	}
	return
}

func (c *Client) RemoveHandler(rid string) {
	c.mu.Lock()
	delete(c.rpcMessages, rid)
	c.mu.Unlock()
}

// SendMessage msgData is a BusinessMessage's RawData
func (c *Client) SendMessage(typeString string, msgData interface{}) (err error) {
	if msgData == nil {
		return
	}
	if c.isClosed() {
		return
	}
	msg, err := CreateWsMessage(typeString, msgData)
	if err != nil {
		return
	}
	select {
	case c.sent <- msg:
	case <-c.closeChan:
		return errors.New("client is closed")
	case <-time.After(time.Millisecond * 100):
		return errors.New("client messages queue is full")
	}
	return
}

func (c *Client) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	close(c.closeChan)
	close(c.sent)
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.closed = true
}

func (c *Client) connect() (err error) {
	dial := websocket.DefaultDialer
	dial.HandshakeTimeout = time.Second * 10
	url, requestHeader := c.config.Url, c.config.RequestHeader
	conn, _, err := dial.Dial(url, requestHeader)
	if err != nil {
		return
	}
	c.setWsConn(conn)
	go c.runReadPump()
	go c.runWritePump()
	return
}

func (c *Client) getBusinessHandlers() map[string]ClientBusinessHandler {
	c.mu.RLock()
	mv := make(map[string]ClientBusinessHandler)
	for k, v := range c.businessHandlers {
		mv[k] = v
	}
	c.mu.RUnlock()
	return mv
}

func (c *Client) getCallHandlers(rid string) (handler ws_rpc.CallHandler, ok bool) {
	c.mu.RLock()
	handler, ok = c.rpcMessages[rid]
	c.mu.RUnlock()
	return
}

func (c *Client) getWsConn() *websocket.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *Client) handleBusinessMessage(biMessage *BusinessMessage) {
	wsMsg := ws_rpc.NewMessage(biMessage.Data, c)
	_e := wsMsg.UnmarshalCallMsg()
	if _e != nil {
		c.runBusinessHandler(&ClientContext{Client: c, Message: biMessage})
		return
	}
	handler, ok := c.getCallHandlers(wsMsg.Msg().RequestID)
	if !ok {
		return
	}
	handler.SetMsg(wsMsg.Msg())
	handler.HasRead()
}

func (c *Client) incrementReconnectTimes() {
	c.mu.Lock()
	c.reconnectTimes++
	c.mu.Unlock()
}

func (c *Client) isClosed() (closed bool) {
	c.mu.RLock()
	closed = c.closed
	c.mu.RUnlock()
	return
}

func (c *Client) isReconnectTimesOver() (over bool) {
	c.mu.RLock()
	over = c.reconnectTimes >= uint32(c.config.ReconnectMaxTimes)
	c.mu.RUnlock()
	return
}

func (c *Client) reconnect() error {
	if c.isClosed() {
		return errors.New("client is closed")
	}
	if c.config.DisableReconnect {
		return errors.New("reconnect is disabled")
	}
	for {
		if c.config.ReconnectMaxTimes > 0 && c.isReconnectTimesOver() {
			if c.config.OnReconnectFail != nil {
				c.config.OnReconnectFail(c)
			}
			return errors.New("reconnect times is over")
		}
		time.Sleep(c.config.ReconnectInterval)
		c.incrementReconnectTimes()
		if e := c.connect(); e == nil {
			if c.config.OnReconnected != nil {
				c.config.OnReconnected(c)
			}
			c.resetReconnectTimes()
			return nil
		}
	}
}

func (c *Client) resetReconnectTimes() {
	c.mu.Lock()
	c.reconnectTimes = 0
	c.mu.Unlock()
}

func (c *Client) runBusinessHandler(ctx *ClientContext) {
	if handler, has := c.getBusinessHandlers()[ctx.Message.Type]; has {
		handler(ctx)
	}
}

func (c *Client) runReadPump() {
	for {
		if c.isClosed() {
			return
		}
		msg, err := c.runReadPumpGetMessage()
		if err != nil {
			if !wsConnIsClosedError(err) {
				StdLogger.Printf("runReadPumpGetMessage error:%v ; closed:%v", err, c.isClosed())
			}
			c.sendCloseChan()
			if c.config.OnClose != nil {
				c.config.OnClose(c)
			}
			if e := c.reconnect(); e != nil {
				c.close()
			}
			return
		}
		if e := c.runReadPumpHandleMessage(msg); e != nil {
			StdLogger.Printf("runReadPumpHandleMessage error:%v\n", err)
		}
	}
}

func (c *Client) runReadPumpGetMessage() (*WsMessage, error) {
	conn := c.getWsConn()
	if conn == nil {
		return nil, errors.New("conn is nil")
	}
	messageType, messageData, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	return &WsMessage{Type: messageType, Data: messageData}, nil
}

func (c *Client) runReadPumpHandleMessage(msg *WsMessage) (err error) {
	messageType, messageData := msg.Type, msg.Data
	if messageType != websocket.TextMessage {
		return
	}
	messageData = bytes.TrimSpace(messageData)
	biMessage, err := DecodeBiMessage(messageData)
	if err != nil {
		return
	}
	c.handleBusinessMessage(biMessage)
	return
}

func (c *Client) runWritePump() {
	for {
		if c.isClosed() {
			return
		}
		select {
		case message, ok := <-c.sent:
			conn := c.getWsConn()
			if conn == nil {
				return
			}
			if !ok {
				_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := conn.WriteMessage(message.Type, message.Data)
			if err != nil {
				StdLogger.Printf("runWritePump error:%v\n", err)
			}
		case <-c.closeChan:
			return
		}
	}
}

func (c *Client) sendCloseChan() {
	if c.isClosed() {
		return
	}
	c.closeChan <- 1
	conn := c.getWsConn()
	if conn != nil {
		_ = conn.Close()
	}
}

func (c *Client) setWsConn(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn = conn
}

type ClientBusinessHandler func(*ClientContext)

type ClientConfig struct {
	Url string

	RequestHeader http.Header

	// DisableReconnect use reconnect or not
	// default is false
	DisableReconnect bool

	// ReconnectInterval reconnect interval
	// default is 5s
	ReconnectInterval time.Duration

	// ReconnectMaxTimes reconnect max times
	// default is 0, means no limit
	ReconnectMaxTimes int

	OnConnected func(*Client)

	OnClose func(*Client)

	OnReconnectFail func(*Client)

	OnReconnected func(*Client)
}

func NewClient(cfg *ClientConfig) *Client {
	cl := getDefaultClient()
	prepareClientConfig(cfg)
	cl.config = cfg
	return cl
}

func getDefaultClient() *Client {
	return &Client{
		closeChan:        make(chan byte, 1),
		sent:             make(chan *WsMessage, 256),
		businessHandlers: make(map[string]ClientBusinessHandler),
		rpcMessages:      make(map[string]ws_rpc.CallHandler),
	}
}

func prepareClientConfig(cfg *ClientConfig) {
	if cfg == nil {
		panic("config is nil")
	}
	if cfg.Url == "" {
		panic("url is empty")
	}

	if cfg.ReconnectInterval == 0 {
		cfg.ReconnectInterval = 5 * time.Second
	}

	if cfg.ReconnectMaxTimes < 0 {
		cfg.ReconnectMaxTimes = 0
	}
}

func wsConnIsClosedError(err error) bool {
	if err == nil {
		return false
	}
	if websocket.IsCloseError(
		err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseAbnormalClosure,
	) {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return false
}
