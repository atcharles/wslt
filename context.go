package wslt

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/atcharles/wslt/ws_rpc"
)

type (
	Context struct {
		*Connection
		Message *BusinessMessage
	}

	ClientContext struct {
		*Client
		Message *BusinessMessage
	}

	WebContext struct {
		r *http.Request
		w http.ResponseWriter

		dataByte []byte
		err      error
	}
)

func (ctx *Context) BindCallRequest() (msg *ws_rpc.CallMsg, err error) {
	callMsg, ok := ws_rpc.ValidCallMsg(ctx.Message.Data)
	if !ok {
		err = errors.New("rpc call: invalid call request")
		return
	}
	msg = callMsg
	return
}

// SendCallBackMsg Sending synchronous messages
func (ctx *Context) SendCallBackMsg(callMsg *ws_rpc.CallMsg, data interface{}) error {
	callMsg.Result, _ = json.Marshal(data)
	return ctx.SendBusinessMessage(ctx.Message.Type, callMsg)
}

func (c *WebContext) IsWebSocket() bool {
	if strings.Contains(strings.ToLower(c.r.Header.Get("Connection")), "upgrade") &&
		strings.ToLower(c.r.Header.Get("Upgrade")) == "websocket" {
		return true
	}
	return false
}

func (c *WebContext) JSON(code int, args ...interface{}) {
	returnJson := new(ReturnJSON)
	var data interface{}
	if len(args) > 0 {
		data = args[0]
	}
	returnJson.Set(code, data)
	//c.w.Header().Set("Sec-Websocket-Version", "13")
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.Header().Set("X-Content-Type-Options", "nosniff")
	c.w.WriteHeader(code)
	c.dataByte, c.err = json.Marshal(returnJson)
	if c.err != nil {
		return
	}
	_, c.err = fmt.Fprintln(c.w, string(c.dataByte))
}
