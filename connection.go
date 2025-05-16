package wslt

import (
	"bytes"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type (
	//Connection a websocket connection
	Connection struct {
		mu sync.RWMutex

		userID string

		sessionID string

		ip string

		conn *websocket.Conn

		sent chan *WsMessage

		closed bool

		closeChan chan struct{}

		ws *WSocket
	}
)

func (c *Connection) Close() { c.close() }

func (c *Connection) GetConn() (conn *websocket.Conn) {
	c.mu.RLock()
	conn = c.conn
	c.mu.RUnlock()
	return
}

func (c *Connection) IpAddress() string { return c.ip }

func (c *Connection) IsClosed() (closed bool) {
	c.mu.RLock()
	closed = c.closed
	c.mu.RUnlock()
	return
}

func (c *Connection) SendBusinessMessage(businessType string, businessMessage interface{}) (err error) {
	if businessMessage == nil {
		return
	}
	wMsg, err := CreateWsMessage(businessType, businessMessage)
	if err != nil {
		return
	}
	return c.SendWsMessage(wMsg)
}

func (c *Connection) SendWsMessage(msg *WsMessage) (err error) {
	if c.IsClosed() {
		return errors.New("connection is closed")
	}
	select {
	case c.sent <- msg:
	case <-c.closeChan:
		err = errors.New("connection is closed")
		return
	case <-time.After(time.Millisecond * 100):
		err = errors.New("connection messages queue is full")
	}
	return
}

func (c *Connection) SessionID() string { return c.sessionID }

func (c *Connection) SetUserID(userID string) { c.userID = userID }

func (c *Connection) UserID() string { return c.userID }

func (c *Connection) Ws() *WSocket { return c.ws }

func (c *Connection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.sent)
	close(c.closeChan)
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.ws.unregister <- c
}

func (c *Connection) readPumpReadMessage() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("readPumpReadMessage panic")
		}
	}()

	if c.IsClosed() {
		return errors.New("connection is closed")
	}
	messageType, messageData, err := c.conn.ReadMessage()
	if err != nil {
		return
	}
	if messageType != websocket.TextMessage {
		return
	}
	messageData = bytes.TrimSpace(bytes.Replace(messageData, []byte{'\n'}, []byte{' '}, -1))
	biMessage, e := DecodeBiMessage(messageData)
	if e != nil {
		return
	}
	c.ws.runReadHandle(&Context{Connection: c, Message: biMessage})
	return
}

func (c *Connection) register() {
	go c.runReadPump()
	go c.runWritePump()
	c.ws.register <- c
}

func (c *Connection) runReadPump() {
	for {
		if c.IsClosed() {
			return
		}
		select {
		case <-c.closeChan:
			return
		default:
			if err := c.readPumpReadMessage(); err != nil {
				StdLogger.Printf("read client sessionID: %s,remoteAddr: %s, error: %s", c.sessionID, c.ip, err.Error())
				/*if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					StdLogger.Printf("readPumpReadMessage error: %s", err.Error())
				}
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					c.close()
					return
				}*/
				c.close()
				return
			}
		}
	}
}

func (c *Connection) runWritePump() {
	c.conn.EnableWriteCompression(true)
	_ = c.conn.SetCompressionLevel(6)
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		//log.Println("收到客户端的Pong")
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		if c.IsClosed() {
			return
		}
		select {
		case <-c.closeChan:
			return
		case message, ok := <-c.sent:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := writeMessage2conn(c.conn, message); err != nil {
				StdLogger.Printf("runWritePump write error: %v", err)
				return
			}
		case <-ticker.C:
			c.sent <- &WsMessage{Type: websocket.PingMessage}
		}
	}
}

func newConnection(conn *websocket.Conn, ws *WSocket) (con *Connection) {
	return &Connection{
		ws:        ws,
		sessionID: nanoid(),
		conn:      conn,
		sent:      make(chan *WsMessage, 100*KB),
		closed:    false,
		closeChan: make(chan struct{}),
	}
}

func writeMessage2conn(conn *websocket.Conn, msg *WsMessage) (err error) {
	if conn == nil {
		return errors.New("conn is nil")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
	err = conn.WriteMessage(msg.Type, msg.Data)
	return
}
