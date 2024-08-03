package wslt

import (
	"bytes"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/atcharles/wslt/ws_rpc"

	"github.com/gorilla/websocket"
)

type (
	Client struct {
		mu sync.RWMutex

		conn *websocket.Conn

		closed bool

		closeChan chan byte

		sent chan *WsMessage

		businessHandlers map[string]ClientBusinessHandler

		rpcMessages map[string]ws_rpc.CallHandler
	}
)

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

func (c *Client) Dial(urlStr string, requestHeader http.Header) (err error) {
	dial := websocket.DefaultDialer
	dial.HandshakeTimeout = time.Second * 10
	c.conn, _, err = dial.Dial(urlStr, requestHeader)
	return
}

func (c *Client) GetHandler(rid string) (handler ws_rpc.CallHandler, ok bool) {
	c.mu.Lock()
	handler, ok = c.rpcMessages[rid]
	c.mu.Unlock()
	return
}

func (c *Client) IsClosed() (closed bool) {
	c.mu.RLock()
	closed = c.closed
	c.mu.RUnlock()
	return
}

// ReadHandle 设置客户端读取操作函数
func (c *Client) ReadHandle(typeStr string, handler ClientBusinessHandler) {
	c.mu.Lock()
	c.businessHandlers[typeStr] = handler
	c.mu.Unlock()
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
	if c.IsClosed() {
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
	c.closed = true
	close(c.closeChan)
	close(c.sent)
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

// getHandlers ...
func (c *Client) getHandlers() map[string]ClientBusinessHandler {
	c.mu.RLock()
	mv := make(map[string]ClientBusinessHandler)
	for k, v := range c.businessHandlers {
		mv[k] = v
	}
	c.mu.RUnlock()
	return mv
}

func (c *Client) readPump() {
	defer c.close()
	for {
		select {
		case <-c.closeChan:
			return
		default:
			if err := c.readPumpReadMessage(); err != nil {
				if websocket.IsUnexpectedCloseError(
					err,
					websocket.CloseNormalClosure,
					websocket.CloseGoingAway,
					websocket.CloseAbnormalClosure,
				) {
					StdLogger.Printf("readPump error:%s\n", err.Error())
				}
				return
			}
		}
	}
}

func (c *Client) readPumpReadMessage() (err error) {
	messageType, messageData, err := c.conn.ReadMessage()
	if err != nil {
		return
	}
	if messageType != websocket.TextMessage {
		return
	}
	messageData = bytes.TrimSpace(messageData)
	biMessage, e := DecodeBiMessage(messageData)
	if e != nil {
		return
	}
	func() {
		wsMsg := ws_rpc.NewMessage(biMessage.Data, c)
		_e := wsMsg.UnmarshalCallMsg()
		if _e != nil {
			c.runReadHandle(&ClientContext{Client: c, Message: biMessage})
			return
		}
		handler, ok := c.GetHandler(wsMsg.Msg().RequestID)
		if !ok {
			return
		}
		handler.SetMsg(wsMsg.Msg())
		handler.HasRead()
	}()
	return
}

func (c *Client) runReadHandle(ctx *ClientContext) {
	if handler, has := c.getHandlers()[ctx.Message.Type]; has {
		handler(ctx)
	}
}

func (c *Client) writePump() {
	defer c.close()
	for {
		select {
		case message, ok := <-c.sent:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				//sent channel is closed
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if message.Type != websocket.TextMessage {
				continue
			}
			if err := c.conn.WriteMessage(message.Type, message.Data); err != nil {
				StdLogger.Printf("writePump error:%s\n", err.Error())
				return
			}
		case <-c.closeChan:
			return
		}
	}
}

type ClientBusinessHandler func(*ClientContext)

func NewClient(urlStr string, requestHeader http.Header) (ws *Client, err error) {
	ws = &Client{
		conn:             nil,
		closed:           false,
		closeChan:        make(chan byte, 1),
		sent:             make(chan *WsMessage, 256),
		businessHandlers: make(map[string]ClientBusinessHandler),
		rpcMessages:      make(map[string]ws_rpc.CallHandler),
	}
	if err = ws.Dial(urlStr, requestHeader); err != nil {
		return
	}
	go ws.readPump()
	go ws.writePump()
	return
}
