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

		ws *WSocket

		connector ItfConnector

		sessionID string

		conn *websocket.Conn

		sent chan *WsMessage

		closed bool

		closeChan chan struct{}
	}
)

func (c *Connection) Close() {
	c.close()
}

func (c *Connection) Connector() ItfConnector {
	return c.connector
}

func (c *Connection) GetConn() (conn *websocket.Conn) {
	c.mu.RLock()
	conn = c.conn
	c.mu.RUnlock()
	return
}

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

// SendWsMessage ...
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

func (c *Connection) SessionID() string {
	return c.sessionID
}

func (c *Connection) Ws() *WSocket {
	return c.ws
}

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

	wSocket.unregister <- c
	globalSession.removeChan <- &SessionData{
		ConnectorID: c.connector.GetID(),
		SessionID:   c.sessionID,
	}
}

func (c *Connection) firstReceive() {
	c.connector = c.connector.NewInstance()
	globalSession.addChan <- &SessionData{ConnectorID: c.connector.GetID(), SessionID: c.sessionID}
	wSocket.register <- c
	go c.readPump()
	go c.writePump()
	c.connector.SetConnection(c)
}

func (c *Connection) readPump() {
	//************IMPORT****************
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
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
					StdLogger.Printf("readPumpReadMessage error: %s", err.Error())
				}
				return
			}
		}
	}
}

func (c *Connection) readPumpReadMessage() (err error) {
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

func (c *Connection) sendOnceAndClose(msgType string, data interface{}) (err error) {
	defer c.close()
	msg, err := CreateWsMessage(msgType, data)
	if err != nil {
		return
	}
	if err = c.conn.WriteMessage(msg.Type, msg.Data); err != nil {
		return
	}
	return
}

func (c *Connection) writePump() {
	c.conn.EnableWriteCompression(true)
	_ = c.conn.SetCompressionLevel(6)

	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()
	for {
		select {
		case <-c.closeChan:
			return
		case message, ok := <-c.sent:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if message.Type != websocket.TextMessage {
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message.Data); err != nil {
				StdLogger.Printf("writePump write error: %v", err)
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				StdLogger.Printf("writePump ping error: %v", err)
				return
			}
		}
	}
}

func newConnection(connector ItfConnector, conn *websocket.Conn) (con *Connection) {
	con = &Connection{
		mu:        sync.RWMutex{},
		ws:        New(),
		connector: connector,
		sessionID: nanoid(),
		conn:      conn,
		sent:      make(chan *WsMessage, 100*KB),
		closed:    false,
		closeChan: make(chan struct{}),
	}
	con.firstReceive()
	return
}
