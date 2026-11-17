package wslt

import "time"

type AfterConnectFunc func(c *Connection)

type AuthChecker func(auth string) (userID string, err error)

type BusinessHandlers map[string]BusinessHandler

type WsOptionFunc = func(w *WSocket)

func WithAfterConnectFunc(a AfterConnectFunc) WsOptionFunc {
	return func(w *WSocket) {
		w.afterConnectFunc = a
	}
}

func WithAuthChecker(a AuthChecker) WsOptionFunc {
	return func(w *WSocket) {
		w.authChecker = a
	}
}

func WithBusinessHandlers(b BusinessHandlers) WsOptionFunc {
	return func(w *WSocket) {
		for k, v := range b {
			w.AddBusinessHandler(k, v)
		}
	}
}

// WithBroadcastQueueSize sets broadcast queue capacity. Default: 1024; nonpositive values are ignored.
func WithBroadcastQueueSize(size int) WsOptionFunc {
	return func(w *WSocket) {
		if size > 0 {
			w.broadcastQueueSize = size
		}
	}
}

// WithConnectionQueueSize sets each peer's queue capacity. Default: 256; nonpositive values are ignored.
func WithConnectionQueueSize(size int) WsOptionFunc {
	return func(w *WSocket) {
		if size > 0 {
			w.connectionQueueSize = size
		}
	}
}

// WithBroadcastTimeout sets SendToAll's queue wait. Default: 2 seconds; nonpositive values are ignored.
func WithBroadcastTimeout(timeout time.Duration) WsOptionFunc {
	return func(w *WSocket) {
		if timeout > 0 {
			w.broadcastTimeout = timeout
		}
	}
}
