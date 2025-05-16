package wslt

import (
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/atcharles/wslt/internal"
	"github.com/atcharles/wslt/internal/wsc"
	"github.com/atcharles/wslt/ws_rpc"

	"github.com/gorilla/websocket"
)

type Client struct {
	mu sync.RWMutex

	connection *wsc.Connection

	businessHandlers map[string]ClientBusinessHandler

	rpcMessages map[string]ws_rpc.CallHandler

	config *ClientConfig
}

func (c *Client) AddBusinessHandler(typeStr string, handler ClientBusinessHandler) {
	c.mu.Lock()
	c.businessHandlers[typeStr] = handler
	c.mu.Unlock()
}

func (c *Client) AddCallHandler(rid string, handler ws_rpc.CallHandler) {
	c.mu.Lock()
	c.rpcMessages[rid] = handler
	c.mu.Unlock()
}

func (c *Client) Dial() (err error) {
	if err = c.connect(); err != nil {
		return
	}
	if c.config.OnConnected != nil {
		c.config.OnConnected(c)
	}
	return
}

func (c *Client) GetConnection() *wsc.Connection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connection
}

func (c *Client) RemoveHandler(rid string) {
	c.mu.Lock()
	delete(c.rpcMessages, rid)
	c.mu.Unlock()
}

// SendMessage msgData is a BusinessMessage's RawData
func (c *Client) SendMessage(typeString string, msgData interface{}) (err error) {
	msg, err := CreateWsMessage(typeString, msgData)
	if err != nil {
		return
	}
	c.GetConnection().SendWsMessage(&internal.WsMessage{Type: msg.Type, Data: msg.Data})
	return
}

func (c *Client) SetConnection(conn *wsc.Connection) {
	c.mu.Lock()
	c.connection = conn
	c.mu.Unlock()
}

func (c *Client) connect() (err error) {
	dial := websocket.DefaultDialer
	dial.HandshakeTimeout = time.Second * 10
	url, requestHeader := c.config.Url, c.config.RequestHeader
	conn, _, err := dial.Dial(url, requestHeader)
	if err != nil {
		return
	}
	c.SetConnection(wsc.NewConnection(conn, wsc.WithOnReceive(c.handleReceive), wsc.WithLogger(StdLogger), wsc.WithOnDone(c.reconnect),
		wsc.WithOnReadError(func(err error) {
			if !wsConnIsClosedError(err) {
				StdLogger.Printf("WithOnReadError websocket connection closed, err:%v\n", err)
			}
		}),
		wsc.WithOnWriteError(func(err error) {
			if !wsConnIsClosedError(err) {
				StdLogger.Printf("WithOnWriteError websocket connection closed, err:%v\n", err)
			}
		}),
	))
	return
}

func (c *Client) getBusinessHandlers() map[string]ClientBusinessHandler {
	c.mu.RLock()
	mv := make(map[string]ClientBusinessHandler)
	for k, v := range c.businessHandlers {
		mv[k] = v
	}
	c.mu.RUnlock()
	return mv
}

func (c *Client) getCallHandlers(rid string) (handler ws_rpc.CallHandler, ok bool) {
	c.mu.RLock()
	handler, ok = c.rpcMessages[rid]
	c.mu.RUnlock()
	return
}

func (c *Client) handleReceive(ms *internal.WsMessage) {
	biMessage, err := DecodeBiMessage(ms.Data)
	if err != nil {
		return
	}
	if handler, has := c.getBusinessHandlers()[biMessage.Type]; has {
		handler(&ClientContext{Client: c, Message: biMessage})
		return
	}

	rpcMsg := ws_rpc.NewMessage(biMessage.Data, c)
	if err = rpcMsg.UnmarshalCallMsg(); err != nil {
		return
	}
	handler, ok := c.getCallHandlers(rpcMsg.Msg().RequestID)
	if !ok {
		return
	}
	handler.SetMsg(rpcMsg.Msg())
	handler.HasRead()
}

func (c *Client) reconnect() {
	if c.config.DisableReconnect {
		return
	}

	var err error
	times := 0

	for {
		if c.config.ReconnectMaxTimes > 0 && times >= c.config.ReconnectMaxTimes {
			if c.config.OnReconnectFail != nil {
				c.config.OnReconnectFail(c)
			}
			return
		}
		if err = c.connect(); err == nil {
			if c.config.OnReconnected != nil {
				c.config.OnReconnected(c)
			}
			StdLogger.Printf("reconnect success\n")
			break
		}
		StdLogger.Printf("reconnect error:%v\n", err)
		time.Sleep(c.config.ReconnectInterval)
		times++
	}
}

type ClientBusinessHandler func(*ClientContext)

type ClientConfig struct {
	Url string

	RequestHeader http.Header

	// DisableReconnect use reconnect or not
	// default is false
	DisableReconnect bool

	// ReconnectInterval reconnect interval
	// default is 5s
	ReconnectInterval time.Duration

	// ReconnectMaxTimes reconnect max times
	// default is 0, means no limit
	ReconnectMaxTimes int

	OnConnected func(*Client)

	OnClose func(*Client)

	OnReconnectFail func(*Client)

	OnReconnected func(*Client)
}

func NewClient(cfg *ClientConfig) *Client {
	cl := getDefaultClient()
	prepareClientConfig(cfg)
	cl.config = cfg
	return cl
}

func getDefaultClient() *Client {
	return &Client{
		businessHandlers: make(map[string]ClientBusinessHandler),
		rpcMessages:      make(map[string]ws_rpc.CallHandler),
	}
}

func prepareClientConfig(cfg *ClientConfig) {
	if cfg == nil {
		panic("config is nil")
	}
	if cfg.Url == "" {
		panic("url is empty")
	}

	if cfg.ReconnectInterval == 0 {
		cfg.ReconnectInterval = 5 * time.Second
	}

	if cfg.ReconnectMaxTimes < 0 {
		cfg.ReconnectMaxTimes = 0
	}
}

func wsConnIsClosedError(err error) bool {
	if err == nil {
		return false
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return false
}
