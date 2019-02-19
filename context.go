package wslt

import (
	"fmt"
	"net/http"
	"strings"
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

func (c *WebContext) IsWebSocket() bool {
	if strings.Contains(strings.ToLower(c.r.Header.Get("Connection")), "upgrade") &&
		strings.ToLower(c.r.Header.Get("Upgrade")) == "websocket" {
		return true
	}
	return false
}

func (c *WebContext) JSON(code int, data interface{}) {
	returnJson := new(ReturnJSON)
	returnJson.Set(code, data)
	c.w.WriteHeader(code)
	c.w.Header().Set("Sec-Websocket-Version", "13")
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.Header().Set("X-Content-Type-Options", "nosniff")
	c.w.WriteHeader(code)
	c.dataByte, c.err = json.Marshal(returnJson)
	if c.err != nil {
		return
	}
	_, c.err = fmt.Fprintln(c.w, string(c.dataByte))
}
