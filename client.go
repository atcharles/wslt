package wslt

import (
	"bytes"
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

func (c *Client) Conn() *websocket.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

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
	msg, err = CreateWsMessage(typeString, msgData)
	if err != nil {
		return
	}
	select {
	case c.sent <- msg:
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
	c.mu.Lock()
	if handler, has := c.businessHandlers[ctx.Message.StringType]; has {
		go handler(ctx)
	}
	c.mu.Unlock()
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
		if err = c.Conn().SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			return
		}
		select {
		default:
		case message, ok := <-c.sent:
			if !ok {
				//sent closed
				if c.IsClosed() {
					return
				}
				err = c.Conn().WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err = c.Conn().WriteMessage(message.MessageType, message.Data); err != nil {
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
		defer func() {
			if err != nil {
				StdLogger.Printf("readPump error:%s\n", err.Error())
			} else {
				StdLogger.Printf("readPump return whith no error")
			}
		}()
		c.close()
	}()
	for {
		var (
			messageType int
			messageData []byte
			biMessage   *BusinessMessage
		)
		select {
		case <-c.closeChan:
			return
		default:
			messageType, messageData, err = c.Conn().ReadMessage()
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
			ctx := &ClientContext{
				Client:  c,
				Message: biMessage,
			}
			c.runReadHandle(ctx)
		}
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
