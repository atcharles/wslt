package wslt

import (
	"os"
	"os/signal"
	"syscall"

	jsoniter "github.com/json-iterator/go"
	"github.com/json-iterator/go/extra"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

/*func objectName(obj interface{}) string {
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
}*/

//func removeSliceInt64(rs *[]int64, val int64) {
//	sl := *rs
//	for i, v := range sl {
//		if v == val {
//			*rs = append(sl[:i], sl[i+1:]...)
//			return
//		}
//	}
//}

func init() {
	extra.RegisterFuzzyDecoders()
	//extra.SetNamingStrategy(extra.LowerCaseWithUnderscores)
	//extra.SupportPrivateFields()
	json = jsoniter.ConfigFastest
}

var json jsoniter.API

func WaitSignal() {
	notifier := make(chan os.Signal, 1)
	signal.Notify(notifier,
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTSTP,
	)
	<-notifier
}

func nanoid() string {
	return gonanoid.Must(21)
}
