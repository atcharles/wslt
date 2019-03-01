package ws_rpc

import (
	jsoniter "github.com/json-iterator/go"
	"github.com/json-iterator/go/extra"
)

var (
	json = jsoniter.ConfigFastest
)

func init() {
	extra.RegisterFuzzyDecoders()
}
