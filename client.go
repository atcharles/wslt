package wslt

import (
	"bytes"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	wsClient *Client
)

func NewClient() *Client {
	if wsClient == nil {
		wsClient = new(Client)
		wsClient.reset()
	}
	return wsClient
}

type ClientBusinessHandler func(*ClientContext)

type (
	Client struct {
		mu   sync.Mutex
		conn *websocket.Conn

		closed    bool
		CloseChan chan byte

		sent chan *WsMessage

		businessHandlers map[string]ClientBusinessHandler
	}
)

func (c *Client) Reset() {
	c.reset()
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
		c.close()
		return
	}
}

func (c *Client) Dial(urlStr string) (err error) {
	c.conn, _, err = websocket.DefaultDialer.Dial(urlStr, nil)
	return
}

func (c *Client) Pump() {
	go c.readPump()
	go c.writePump()
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

func (c *Client) receiveDeadline() (err error) {
	if err = c.conn.SetReadDeadline(time.Now().Add(writeWait)); err != nil {
		return
	}
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(pongWait)) })
	return
}

func (c *Client) writePump() {
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
		case <-c.CloseChan:
			return
		case <-ticker.C:
			if err = c.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func (c *Client) readPump() {
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
		case <-c.CloseChan:
			return
		default:
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
			ctx := &ClientContext{
				Client:  c,
				Message: biMessage,
			}
			c.runReadHandle(ctx)
		}
	}
}

func (c *Client) reset() {
	*c = Client{
		mu:               sync.Mutex{},
		conn:             nil,
		closed:           false,
		CloseChan:        make(chan byte, 1),
		sent:             make(chan *WsMessage, 256),
		businessHandlers: make(map[string]ClientBusinessHandler),
	}
}

func (c *Client) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.CloseChan)
	close(c.sent)
}
