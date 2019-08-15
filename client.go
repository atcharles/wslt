package wslt

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/atcharles/wslt/ws_rpc"

	"github.com/gorilla/websocket"
)

func NewClient(urlStr string) (ws *Client, err error) {
	ws = &Client{
		conn:             nil,
		closed:           false,
		closeChan:        make(chan byte, 1),
		sent:             make(chan *WsMessage, 256),
		businessHandlers: make(map[string]ClientBusinessHandler),
		rpcMessages:      make(map[string]ws_rpc.CallHandler),
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
		mu   sync.RWMutex
		conn *websocket.Conn

		closed    bool
		closeChan chan byte

		sent chan *WsMessage

		businessHandlers map[string]ClientBusinessHandler

		rpcMessages map[string]ws_rpc.CallHandler
	}
)

func (c *Client) AddCallHandler(rid string, handler ws_rpc.CallHandler) {
	c.mu.Lock()
	c.rpcMessages[rid] = handler
	c.mu.Unlock()
}

func (c *Client) RemoveHandler(rid string) {
	c.mu.Lock()
	delete(c.rpcMessages, rid)
	c.mu.Unlock()
}

func (c *Client) GetHandler(rid string) (handler ws_rpc.CallHandler, ok bool) {
	c.mu.Lock()
	handler, ok = c.rpcMessages[rid]
	c.mu.Unlock()
	return
}

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

//ReadHandle 设置客户端读取操作函数
func (c *Client) ReadHandle(typeStr string, handler ClientBusinessHandler) {
	c.mu.Lock()
	c.businessHandlers[typeStr] = handler
	c.mu.Unlock()
}

//SendMessage msgData is a BusinessMessage's RawData
func (c *Client) SendMessage(typeString string, msgData interface{}) (err error) {
	var msg *WsMessage
	defer func() {
		if err != nil {
			StdLogger.Printf("发送数据失败:%s\n", err.Error())
		}
	}()
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
	dial := websocket.DefaultDialer
	dial.HandshakeTimeout = time.Second * 3
	c.conn, _, err = dial.Dial(urlStr, nil)
	return
}

//getHandlers ...
func (c *Client) getHandlers() map[string]ClientBusinessHandler {
	c.mu.RLock()
	hs := c.businessHandlers
	c.mu.RUnlock()
	return hs
}

func (c *Client) runReadHandle(ctx *ClientContext) {
	ic := ctx.clone()
	if handler, has := c.getHandlers()[ic.Message.StringType]; has {
		inHandler := handler
		inHandler(ic)
	}
}

func (c *Client) writePump() {
	var err error
	defer func() {
		if err != nil {
			StdLogger.Printf("发送数据失败:%s\n", err.Error())
		}
	}()
	for {
		select {
		case message, ok := <-c.sent:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				//sent channel is closed
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
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
	defer func() {
		if err != nil {
			StdLogger.Printf("读取数据失败:%s\n", err.Error())
		}
	}()
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

		wsMsg := ws_rpc.NewMessage(biMessage.Data, c)
		e := wsMsg.UnmarshalCallMsg()
		if e != nil {
			//不是一个call msg
			go c.runReadHandle(&ClientContext{
				Client:  c,
				Message: biMessage,
			})
		} else {
			handler, ok := c.GetHandler(wsMsg.Msg().RequestID)
			if !ok {
				continue
			}
			handler.SetMsg(wsMsg.Msg())
			handler.HasRead()
		}

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
