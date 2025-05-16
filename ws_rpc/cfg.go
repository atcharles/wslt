package ws_rpc

import (
	jsoniter "github.com/json-iterator/go"
	"github.com/json-iterator/go/extra"
)

func init() {
	extra.RegisterFuzzyDecoders()
	//extra.SetNamingStrategy(extra.LowerCaseWithUnderscores)
	//extra.SupportPrivateFields()
	json = jsoniter.ConfigFastest
}

var json jsoniter.API
