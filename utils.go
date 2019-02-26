package wslt

import (
	"reflect"
	"runtime"

	jsoniter "github.com/json-iterator/go"
	"github.com/json-iterator/go/extra"
)

var (
	json jsoniter.API
)

func init() {
	extra.RegisterFuzzyDecoders()
	//extra.SetNamingStrategy(extra.LowerCaseWithUnderscores)
	//extra.SupportPrivateFields()
	json = jsoniter.ConfigFastest
}

func removeSliceInt64(rs *[]int64, val int64) {
	var r int
	sl := *rs
	for i, v := range sl {
		if v == val {
			r = i
		}
	}
	*rs = append(sl[:r], sl[r+1:]...)
}

func objectName(obj interface{}) string {
	v := reflect.ValueOf(obj)
	t := v.Type()
	switch t.Kind() {
	case reflect.Ptr:
		return v.Elem().Type().Name()
	case reflect.Struct:
		return t.Name()
	case reflect.Func:
		return runtime.FuncForPC(v.Pointer()).Name()
	}
	return t.String()
}
