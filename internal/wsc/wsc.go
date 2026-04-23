package wsc

import (
	"bytes"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/atcharles/wslt/internal"
)

type Connection struct {
	sync.RWMutex
	onceDone sync.Once
	conn     *websocket.Conn
	isDone   bool
	done     chan struct{}
	msg      chan *internal.WsMessage

	logger internal.Logger

	onReceive    func(*internal.WsMessage)
	onReadError  func(error)
	onWriteError func(error)
	onDone       func()
}

func (c *Connection) Close() { c.Done() }

func (c *Connection) Done() {
	c.onceDone.Do(func() {
		c.Lock()
		defer c.Unlock()
		if c.isDone {
			return
		}
		c.isDone = true
		close(c.done)
		close(c.msg)
		_ = c.conn.Close()
		if c.onDone != nil {
			c.onDone()
		}
	})
}

func (c *Connection) IsDone() bool {
	c.RLock()
	defer c.RUnlock()
	return c.isDone
}

func (c *Connection) Listen() {
	defer c.Done()
	c.conn.SetPingHandler(func(string) error {
		//c.logger.Printf("websocket ping: %s", appData)
		c.SendWsMessage(&internal.WsMessage{Type: websocket.PongMessage})
		return nil
	})
	c.conn.SetCloseHandler(func(code int, text string) error {
		//c.logger.Printf("websocket closed: %d, %s", code, text)
		c.Done()
		return nil
	})

	_ = c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		//c.logger.Printf("websocket pong received from server\n")
		return c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	})

	for {
		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			if c.onReadError != nil {
				c.onReadError(err)
			}
			//c.logger.Printf("read message error: %v", err)
			return
		}
		if msgType != websocket.TextMessage {
			//c.logger.Printf("websocket message type: %d", msgType)
			continue
		}
		if c.onReceive != nil {
			c.onReceive(&internal.WsMessage{Type: msgType, Data: bytes.TrimSpace(data)})
		}
	}
}

func (c *Connection) SendWsMessage(msg *internal.WsMessage) {
	if c.IsDone() {
		return
	}
	c.msg <- msg
}

func (c *Connection) WriteLoop() {
	tk := time.NewTicker(15 * time.Second)
	defer func() {
		tk.Stop()
		c.Done()
	}()
	for {
		select {
		case <-tk.C:
			c.SendWsMessage(&internal.WsMessage{Type: websocket.PingMessage})
		case <-c.done:
			return
		case msg, ok := <-c.msg:
			if !ok {
				return
			}
			//c.logger.Printf("websocket write message: %+#v", msg)
			_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.conn.WriteMessage(msg.Type, msg.Data); err != nil {
				if c.onWriteError != nil {
					c.onWriteError(err)
				}
				//c.logger.Printf("write message error: %v", err)
				return
			}
		}
	}
}

type ConnectionOption func(*Connection)

func NewConnection(conn *websocket.Conn, opts ...ConnectionOption) *Connection {
	c := &Connection{conn: conn, done: make(chan struct{}), msg: make(chan *internal.WsMessage, 10)}
	for _, opt := range opts {
		opt(c)
	}
	go c.Listen()
	go c.WriteLoop()
	return c
}

func WithLogger(logger internal.Logger) ConnectionOption {
	return func(c *Connection) { c.logger = logger }
}

func WithOnDone(f func()) ConnectionOption {
	return func(c *Connection) { c.onDone = f }
}

func WithOnReadError(f func(error)) ConnectionOption {
	return func(c *Connection) { c.onReadError = f }
}

func WithOnReceive(f func(*internal.WsMessage)) ConnectionOption {
	return func(c *Connection) { c.onReceive = f }
}

func WithOnWriteError(f func(error)) ConnectionOption {
	return func(c *Connection) { c.onWriteError = f }
}
