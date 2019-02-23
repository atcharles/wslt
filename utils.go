package wslt

import (
	"reflect"
	"runtime"
)

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
