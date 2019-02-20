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

func (c *Client) CloseChan() chan byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeChan
}

func (c *Client) ReadHandle(typeStr string, handler ClientBusinessHandler) {
	c.mu.Lock()
	c.businessHandlers[typeStr] = handler
	c.mu.Unlock()
}

//msgData is a BusinessMessage's RawData
func (c *Client) SendMessage(typeString string, msgData interface{}) {
	if c.IsClosed() {
		return
	}
	var (
		err error
		msg *WsMessage
	)
	defer func() {
		if err != nil {
			StdLogger.Printf("send message error:%s\n", err.Error())
		}
	}()
	if msgData == nil {
		err = errors.New("nil pointer")
		return
	}
	msg, err = CreateWsMessage(typeString, msgData)
	if err != nil {
		return
	}
	select {
	case c.sent <- msg:
	case <-c.closeChan:
		return
	default:
		StdLogger.Printf("can't set msg to sent channel")
		c.close()
		return
	}
}

func (c *Client) Dial(urlStr string) (err error) {
	c.conn, _, err = websocket.DefaultDialer.Dial(urlStr, nil)
	return
}

func (c *Client) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Client) runReadHandle(ctx *ClientContext) {
	defer func() {
		if p := recover(); p != nil {
			StdLogger.Printf("run handler error:%v\n", p)
		}
	}()
	if handler, has := c.businessHandlers[ctx.Message.StringType]; has {
		go handler(ctx)
	}
}

func (c *Client) writePump() {
	var err error
	defer func() {
		if err != nil {
			StdLogger.Printf("writePump error:%s\n", err.Error())
		} else {
			StdLogger.Printf("wriePump return whith no error")
		}
		c.close()
	}()
	for {
		if err = c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			return
		}
		select {
		case message, ok := <-c.sent:
			if !ok {
				return
			}
			if message.MessageType != websocket.TextMessage {
				StdLogger.Printf("wirte message type is:%d, data is:%s\n",
					message.MessageType, message.Data)
				continue
			}

			StdLogger.Printf("WriteMessage message: %s\n", message.Data)

			if err = c.conn.WriteMessage(websocket.TextMessage, message.Data); err != nil {
				return
			}
		case <-c.closeChan:
			return
		}
	}
}

func (c *Client) readPump() {
	var err error
	defer func() {
		if err != nil {
			StdLogger.Printf("readPump error:%s\n", err.Error())
		} else {
			StdLogger.Printf("readPump return whith no error")
		}
		c.close()
	}()
	for {
		var (
			messageType int
			messageData []byte
			biMessage   *BusinessMessage
		)
		messageType, messageData, err = c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure) {
				//log ...
			}
			StdLogger.Printf("Read error:%s\n", err.Error())
			return
		}
		if messageType != websocket.TextMessage {
			StdLogger.Printf("read message type is:%d,data is:%s\n",
				messageType, messageData)
			continue
		}
		messageData = bytes.TrimSpace(bytes.Replace(messageData, []byte{'\n'}, []byte{' '}, -1))

		StdLogger.Printf("Read Message:%s\n", messageData)

		if biMessage, err = DecodeBiMessage(messageData); err != nil {
			return
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

		//StdLogger.Printf("Read Message:%s\n", messageData)
	}
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
}
