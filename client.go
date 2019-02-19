package wslt

import (
	"bytes"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ClientBusinessHandler func(*ClientContext)

type (
	WsClient struct {
		mu sync.Mutex

		businessHandlers map[string]ClientBusinessHandler
	}

	Client struct {
		mu   sync.Mutex
		conn *websocket.Conn

		ws        *WsClient
		closed    bool
		closeChan chan byte

		sent chan *WsMessage
	}
)

//msgData is a BusinessMessage's RawData
func (c *Client) SendMessage(typeString string, msgData interface{}) {
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
	if err != nil {
		return
	}
	return
}

func (c *Client) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (w *WsClient) runReadHandle(ctx *ClientContext) {
	defer func() {
		if p := recover(); p != nil {
			StdLogger.Printf("run handler error:%v\n", p)
		}
	}()
	w.mu.Lock()
	if handler, has := w.businessHandlers[ctx.Message.StringType]; has {
		go handler(ctx)
	}
	w.mu.Unlock()
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
		case <-c.closeChan:
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
		case <-c.closeChan:
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
			c.ws.runReadHandle(ctx)
		}
	}
}

func (c *Client) reset() {
	*c = Client{
		conn:      nil,
		closed:    false,
		closeChan: make(chan byte, 1),
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
