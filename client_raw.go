package wslt

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func NewRawClient(urlStr string) (ws *RawClient, err error) {
	ws = &RawClient{
		mu:        sync.Mutex{},
		conn:      nil,
		closed:    false,
		closeChan: make(chan byte, 1),
		sent:      make(chan *WsMessage, 256),
		rawChan:   make(chan []byte, 256),
	}
	if err = ws.Dial(urlStr); err != nil {
		err = fmt.Errorf("Client Dial error:%s\n", err.Error())
		return
	}
	go ws.readPump()
	go ws.writePump()
	return
}

type (
	RawClient struct {
		mu        sync.Mutex
		conn      *websocket.Conn
		closed    bool
		closeChan chan byte
		sent      chan *WsMessage
		rawChan   chan []byte
	}
)

func (c *RawClient) CloseChan() <-chan byte {
	return c.closeChan
}

func (c *RawClient) Close() {
	c.close()
}

func (c *RawClient) IsClosed() (closed bool) {
	c.mu.Lock()
	closed = c.closed
	c.mu.Unlock()
	return
}

func (c *RawClient) ReadMessage() (raw []byte, err error) {
	select {
	case raw = <-c.rawChan:
	case <-c.closeChan:
		err = errors.New("链接已关闭")
	}
	return
}

//msgData is a BusinessMessage's RawData
func (c *RawClient) SendMessage(typeString string, msgData interface{}) (err error) {
	var msg *WsMessage
	defer func() {
		if err != nil {
			StdLogger.Printf("发送数据失败:%s\n", err.Error())
		}
	}()
	defer func() {}()
	if c.IsClosed() {
		err = errors.New("client is closed")
		return
	}
	if msgData == nil {
		err = errors.New("数据格式错误,nil pointer")
		return
	}
	msg, err = CreateWsMessage(typeString, msgData)
	if err != nil {
		err = errors.New("数据格式错误")
		return
	}
	select {
	case c.sent <- msg:
	case <-c.closeChan:
		err = errors.New("the sent channel is closed")
		return
	default:
		err = errors.New("the sent channel is full")
	}
	return
}

func (c *RawClient) Dial(urlStr string) (err error) {
	c.conn, _, err = websocket.DefaultDialer.Dial(urlStr, nil)
	return
}

func (c *RawClient) writePump() {
	var err error
	for {
		if err = c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			goto CLOSE
		}
		select {
		case message, ok := <-c.sent:
			if !ok {
				//sent channel is closed
				return
			}
			if message.MessageType != websocket.TextMessage {
				continue
			}
			if err = c.conn.WriteMessage(websocket.TextMessage, message.Data); err != nil {
				if websocket.IsUnexpectedCloseError(err, 1001, 1005, 1006) {
					StdLogger.Printf("wirtePump error:%s\n", err.Error())
				}
				goto CLOSE
			}
		case <-c.closeChan:
			return
		}
	}

CLOSE:
	c.close()
}

func (c *RawClient) readPump() {
	var err error
	for {
		var (
			messageType int
			messageData []byte
		)
		messageType, messageData, err = c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, 1001, 1005, 1006) {
				StdLogger.Printf("readPump error:%s\n", err.Error())
			}
			goto CLOSE
		}
		if messageType != websocket.TextMessage {
			continue
		}
		messageData = bytes.TrimSpace(bytes.Replace(messageData, []byte{'\n'}, []byte{' '}, -1))
		select {
		case c.rawChan <- messageData:
		case <-c.closeChan:
			return
		}
	}

CLOSE:
	c.close()
}

func (c *RawClient) close() {
	_ = c.conn.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.closeChan)
	close(c.sent)
}
