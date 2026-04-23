package wslt

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

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

func (c *RawClient) Close() {
	c.close()
}

func (c *RawClient) CloseChan() <-chan byte {
	return c.closeChan
}

func (c *RawClient) Dial(urlStr string, requestHeader http.Header) (err error) {
	c.conn, _, err = websocket.DefaultDialer.Dial(urlStr, requestHeader)
	return
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

// SendMessage msgData is a BusinessMessage's RawData
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

func (c *RawClient) readPump() {
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
					StdLogger.Printf("runReadPump error:%s\n", err.Error())
				}
				return
			}
		}
	}
}

func (c *RawClient) readPumpReadMessage() (err error) {
	messageType, messageData, err := c.conn.ReadMessage()
	if err != nil {
		return
	}
	if messageType != websocket.TextMessage {
		return
	}
	messageData = bytes.TrimSpace(bytes.Replace(messageData, []byte{'\n'}, []byte{' '}, -1))
	select {
	case c.rawChan <- messageData:
	case <-c.closeChan:
		return
	}
	return
}

func (c *RawClient) writePump() {
	defer c.close()
	for {
		select {
		case message, ok := <-c.sent:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(message.Type, message.Data); err != nil {
				StdLogger.Printf("runWritePump error:%s\n", err.Error())
				return
			}
		case <-c.closeChan:
			return
		}
	}
}

func NewRawClient(urlStr string, requestHeader http.Header) (ws *RawClient, err error) {
	ws = &RawClient{
		mu:        sync.Mutex{},
		conn:      nil,
		closed:    false,
		closeChan: make(chan byte, 1),
		sent:      make(chan *WsMessage, 256),
		rawChan:   make(chan []byte, 256),
	}
	if err = ws.Dial(urlStr, requestHeader); err != nil {
		err = fmt.Errorf("Client Dial error:%s\n", err.Error())
		return
	}
	go ws.readPump()
	go ws.writePump()
	return
}
