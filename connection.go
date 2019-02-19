package wslt

import (
	"bytes"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type (
	//Connection a websocket connection
	Connection struct {
		ws *WSocket

		connector Connector

		sessionID int64

		conn *websocket.Conn

		sent chan *WsMessage

		closed    bool
		closeChan chan byte

		mu sync.Mutex
	}
)

func (c *Connection) SendMessage(businessType string, businessMessage interface{}) (err error) {
	var msg *BusinessMessage
	if msg, err = NewBusinessMessage(businessType, businessMessage); err != nil {
		return
	}
	return c.sendBusinessMessage(msg)
}

func (c *Connection) sendBusinessMessage(msg *BusinessMessage) (err error) {
	if c.IsClosed() {
		return
	}
	var (
		wsMsgData []byte
	)
	if wsMsgData, err = json.Marshal(msg); err != nil {
		return
	}
	wMsg := NewWsMessage(websocket.TextMessage, wsMsgData)
	select {
	case c.sent <- wMsg:
	default:
		//缓冲区写满
		c.close()
	}
	return
}

func (c *Connection) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Connection) add() {
	c.mu.Lock()
	sid := globalSession.GetSid()
	c.sessionID = sid
	wSocket.addConnections(sid, c)
	globalSession.SetConnectors(c.connector.GetID(), sid)
	c.mu.Unlock()
}

func (c *Connection) close() {
	_ = c.conn.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	close(c.sent)
	c.closed = true
	close(c.closeChan)
	if c.sessionID == 0 {
		return
	}
	wSocket.mu.Lock()
	defer wSocket.mu.Unlock()
	delete(wSocket.connections, c.sessionID)
	//remove connector's sid
	globalSession.OnWsClose(c.connector.GetID(), c.sessionID)
}

func (c *Connection) readPump() {
	var err error
	defer func() {
		c.close()
	}()
	for {
		if err = c.receiveDeadline(); err != nil {
			return
		}
		var (
			messageType int
			messageData []byte
			biMessage   *BusinessMessage
		)
		select {
		case <-c.closeChan:
			return
		default:
			if c.IsClosed() {
				return
			}
			messageType, messageData, err = c.conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					//log ...
					err = nil
					StdLogger.Printf("readPump error:%s\n", err.Error())
				}
				return
			}
			if messageType != websocket.TextMessage {
				continue
			}
			messageData = bytes.TrimSpace(bytes.Replace(messageData, []byte{'\n'}, []byte{' '}, -1))
			if biMessage, err = DecodeBiMessage(messageData); err != nil {
				return
			}
			ctx := &Context{
				Connection: c,
				Message:    biMessage,
			}
			c.ws.runReadHandle(ctx)
		}
	}
}

func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	var err error
	defer func() {
		c.close()
		if err != nil {
			StdLogger.Printf("writePump error:%s\n", err.Error())
		}
	}()
	for {
		if err = c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			return
		}
		select {
		case message, ok := <-c.sent:
			if !ok {
				//sent closed
				if c.IsClosed() {
					return
				}
				err = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err = c.conn.WriteMessage(message.MessageType, message.Data); err != nil {
				return
			}
		case <-c.closeChan:
			return
		case <-ticker.C:
			if err = c.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func (c *Connection) receiveDeadline() (err error) {
	if err = c.conn.SetReadDeadline(time.Now().Add(writeWait)); err != nil {
		return
	}
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(pongWait)) })
	return
}

func (c *Connection) firstReceive() (err error) {
	defer func() {
		if err != nil {
			c.close()
		} else {
			_ = c.SendMessage("success", "Login Success!")
		}
	}()
	if err = c.receiveDeadline(); err != nil {
		return
	}
	var (
		msgType int
		data    []byte

		msg *BusinessMessage
	)
	msgType, data, err = c.conn.ReadMessage()
	if err != nil {
		return
	}
	if msgType != websocket.TextMessage {
		return ErrUnauthorized
	}
	msg, err = DecodeBiMessage(data)
	if err != nil {
		return
	}
	if msg.StringType != "login" {
		return ErrUnauthorized
	}
	var secret string
	if err = msg.UnmarshalData(&secret); err != nil {
		return
	}
	if len(secret) == 0 {
		return ErrUnauthorized
	}
	c.connector = c.connector.NewInstance()
	c.connector.SetConnection(c)
	if err = c.connector.CheckAuth(secret, c.conn); err != nil {
		return
	}
	return
}

func newConnection(connector Connector, conn *websocket.Conn) (con *Connection, err error) {
	con = &Connection{
		ws:        wSocket,
		connector: connector,
		sessionID: 0,
		conn:      conn,
		sent:      make(chan *WsMessage, 256),
		closed:    false,
		closeChan: make(chan byte, 1),
		mu:        sync.Mutex{},
	}
	//receive first,check auth
	if err = con.firstReceive(); err != nil {
		err = ErrUnauthorized
		return
	}
	go con.readPump()
	go con.writePump()
	return
}
