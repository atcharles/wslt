package wslt

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func NewClient(urlStr string) (ws *Client, err error) {
	ws = &Client{
		mu:               sync.Mutex{},
		conn:             nil,
		closed:           false,
		closeChan:        make(chan byte, 1),
		sent:             make(chan *WsMessage, 256),
		businessHandlers: make(map[string]ClientBusinessHandler),
	}
	if err = ws.Dial(urlStr); err != nil {
		err = fmt.Errorf("Client Dial error:%s\n", err.Error())
		return
	}
	go ws.readPump()
	go ws.writePump()
	return
}

type ClientBusinessHandler func(*ClientContext)

type (
	Client struct {
		mu   sync.Mutex
		conn *websocket.Conn

		closed    bool
		closeChan chan byte

		sent chan *WsMessage

		businessHandlers map[string]ClientBusinessHandler
	}
)

func (c *Client) CloseChan() <-chan byte {
	return c.closeChan
}

func (c *Client) Close() {
	c.close()
}

func (c *Client) IsClosed() (closed bool) {
	c.mu.Lock()
	closed = c.closed
	c.mu.Unlock()
	return
}

func (c *Client) ReadHandle(typeStr string, handler ClientBusinessHandler) {
	c.mu.Lock()
	c.businessHandlers[typeStr] = handler
	c.mu.Unlock()
}

//msgData is a BusinessMessage's RawData
func (c *Client) SendMessage(typeString string, msgData interface{}) (err error) {
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

func (c *Client) Dial(urlStr string) (err error) {
	c.conn, _, err = websocket.DefaultDialer.Dial(urlStr, nil)
	return
}

func (c *Client) runReadHandle(ctx *ClientContext) {
	c.mu.Lock()
	if handler, has := c.businessHandlers[ctx.Message.StringType]; has {
		handler.run(ctx)
	}
	c.mu.Unlock()
}

func (h ClientBusinessHandler) run(ctx *ClientContext) {
	defer func() {
		if p := recover(); p != nil {
			StdLogger.Printf("run handler error:\nFunc:[%s]:\n%v\n", objectName(h), p)
		}
	}()
	go h(ctx)
}

func (c *Client) writePump() {
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

func (c *Client) readPump() {
	var err error
	for {
		var (
			messageType int
			messageData []byte
			biMessage   *BusinessMessage
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

		if biMessage, err = DecodeBiMessage(messageData); err != nil {
			//读取到格式不正确的数据
			StdLogger.Printf("读取到格式不正确的数据:%s\n", messageData)
			continue
		}
		ctx := &ClientContext{
			Client:  c,
			Message: biMessage,
		}
		c.runReadHandle(ctx)

		select {
		case <-c.closeChan:
			return
		default:
		}
	}

CLOSE:
	c.close()
}

func (c *Client) close() {
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
