package wslt

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
