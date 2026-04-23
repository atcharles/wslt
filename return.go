package wslt

import (
	"net/http"
)

var DefaultReturnJSONValue = &ReturnJSON{}

type ReturnJSON struct {
	Code       int         `json:"code,omitempty"`
	StatusText string      `json:"status_text,omitempty"`
	Message    interface{} `json:"message,omitempty"`
}

func (r *ReturnJSON) Ok(i interface{}) *ReturnJSON {
	return r.Set(http.StatusOK, i)
}

func (r *ReturnJSON) Set(code int, msg interface{}) *ReturnJSON {
	r.Code = code
	r.StatusText = http.StatusText(code)
	r.Message = msg
	return r
}
