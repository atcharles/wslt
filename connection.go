package wslt

import (
	"bytes"
	"errors"
	"fmt"
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
		closeChan chan struct{}

		mu sync.Mutex
	}
)

func (c *Connection) Close() {
	c.close()
}

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
	var msg *BusinessMessage
	if msg, err = NewBusinessMessage(businessType, businessMessage); err != nil {
		err = fmt.Errorf("创建业务数据失败: %s\n", err.Error())
		return
	}
	err = c.sendBusinessMessage(msg)
	if err != nil {
		err = fmt.Errorf("发送数据失败: %s\n", err.Error())
	}
	return
}

func (c *Connection) sendBusinessMessage(msg *BusinessMessage) (err error) {
	if c.IsClosed() {
		return errors.New("链接已关闭")
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
		err = errors.New("链接已关闭")
		return
	default:
		//缓冲区写满
		err = errors.New("can't set msg to sent channel")
	}
	return
}

func (c *Connection) IsClosed() (closed bool) {
	c.mu.Lock()
	closed = c.closed
	c.mu.Unlock()
	return
}

func (c *Connection) GetConn() (conn *websocket.Conn) {
	c.mu.Lock()
	conn = c.conn
	c.mu.Unlock()
	return
}

func (c *Connection) add() {
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
		messageType, messageData, err = c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, 1001, 1005, 1006) {
				StdLogger.Printf("Server readPump error:%s\n", err.Error())
			}
			goto Close
		}
		if messageType != websocket.TextMessage {
			continue
		}
		messageData = bytes.TrimSpace(bytes.Replace(messageData, []byte{'\n'}, []byte{' '}, -1))
		if biMessage, err = DecodeBiMessage(messageData); err != nil {
			StdLogger.Printf("读取到格式不正确的数据:%s\n", messageData)
			continue
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

Close:
	c.close()
}

func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	var err error
	defer func() {
		ticker.Stop()
	}()
	for {
		select {
		case message, ok := <-c.sent:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				err = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				goto Close
			}

			if message.MessageType != websocket.TextMessage {
				continue
			}
			c.conn.EnableWriteCompression(true)
			_ = c.conn.SetCompressionLevel(6)
			if err = c.conn.WriteMessage(websocket.TextMessage, message.Data); err != nil {
				goto Close
			}
		case <-c.closeChan:
			return
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err = c.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				goto Close
			}
		}
	}

Close:
	c.close()
}

func (c *Connection) sendOnce(msgType string, data interface{}) {
	var (
		err error
		msg *WsMessage
	)
	msg, err = CreateWsMessage("failed", data)
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
			c.sendOnce("failed", fmt.Sprintf("Login Failed: %s", err.Error()))
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
		closeChan: make(chan struct{}, 1),
		mu:        sync.Mutex{},
	}
	//receive first,check auth
	if err = con.firstReceive(); err != nil {
		err = ErrUnauthorized
	}
	return
}
