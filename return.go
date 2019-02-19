package wslt

import (
	"net/http"
)

var (
	Return = new(ReturnJSON)
)

type ReturnJSON struct {
	Code       int         `json:"code"`
	StatusText string      `json:"status_text"`
	Message    interface{} `json:"message,omitempty"`
}

func (r *ReturnJSON) Set(code int, msg interface{}) *ReturnJSON {
	r.Code = code
	r.StatusText = http.StatusText(code)
	r.Message = msg
	return r
}

func (r *ReturnJSON) Ok(i interface{}) *ReturnJSON {
	return r.Set(http.StatusOK, i)
}
