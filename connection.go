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

func (c *Connection) Ws() *WSocket {
	return c.ws
}

func (c *Connection) SessionID() int64 {
	return c.sessionID
}

func (c *Connection) Connector() Connector {
	return c.connector
}

func (c *Connection) SendMessage(businessType string, businessMessage interface{}) (err error) {
	if businessMessage == nil {
		return errors.New("nil pointer")
	}
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
	case <-c.closeChan:
		return
	default:
		//缓冲区写满
		StdLogger.Printf("can't set msg to sent channel")
		c.close()
	}
	return
}

func (c *Connection) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Connection) GetConn() *websocket.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

func (c *Connection) add() {
	//c.mu.Lock()
	//defer c.mu.Unlock()
	globalSession.addChan <- c.connector.GetID()
	c.sessionID = <-globalSession.sidChan
	wSocket.register <- c
	go c.readPump()
	go c.writePump()
}

func (c *Connection) close() {
	_ = c.conn.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.sent)
	close(c.closeChan)
	if c.sessionID == 0 {
		return
	}
	wSocket.unregister <- c
	globalSession.removeChan <- &SessionRelationShip{
		connectorID: c.connector.GetID(),
		sids:        []int64{c.sessionID},
	}
}

func (c *Connection) readPump() {
	var err error
	defer func() {
		if err != nil {
			StdLogger.Printf("readPump error:%s\n", err.Error())
		} else {
			//StdLogger.Printf("readPump return whith no error")
		}
		c.close()
	}()
	//************IMPORT****************
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { _ = c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {

		var (
			messageType int
			messageData []byte
			biMessage   *BusinessMessage
		)
		if c.IsClosed() {
			return
		}
		messageType, messageData, err = c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				//log ...
			}
			StdLogger.Printf("ws server ReadMessage error:%s\n", err.Error())
			return
		}
		if messageType != websocket.TextMessage {
			StdLogger.Printf("read message type is:%d,data is:%s\n",
				messageType, messageData)
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
		select {
		case <-c.closeChan:
			return
		default:
		}
	}
}

func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	var err error
	defer func() {
		if err != nil {
			StdLogger.Printf("writePump error:%s\n", err.Error())
		} else {
			//StdLogger.Printf("wriePump return whith no error")
		}
		c.close()
	}()
	for {
		//sent closed
		if c.IsClosed() {
			return
		}
		_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		select {
		case message, ok := <-c.sent:
			if !ok {
				//err = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if message.MessageType != websocket.TextMessage {
				StdLogger.Printf("wirte message type is:%d, data is:%s\n",
					message.MessageType, message.Data)
				continue
			}

			if err = c.conn.WriteMessage(websocket.TextMessage, message.Data); err != nil {
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

func (c *Connection) sendOnce(msgType string, data interface{}) {
	var (
		err error
		msg *WsMessage
	)
	msg, err = CreateWsMessage("failed", "Login Failed!")
	if err != nil {
		return
	}
	if err = c.conn.WriteMessage(msg.MessageType, msg.Data); err != nil {
		return
	}
	_ = c.conn.Close()
}

func (c *Connection) firstReceive() (err error) {
	defer func() {
		if err != nil {
			c.sendOnce("failed", "Login Failed!")
		} else {
			_ = c.SendMessage("success", "Login Success!")
		}
	}()
	c.connector = c.connector.NewInstance()
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
	if err = c.connector.CheckAuth(secret, c.conn); err != nil {
		return
	}
	c.add()
	c.connector.SetConnection(c)
	return
}

func newConnection(connector Connector, conn *websocket.Conn) (con *Connection, err error) {
	con = &Connection{
		ws:        New(),
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
	}
	return
}
