package wslt

import "github.com/gorilla/websocket"

type (
	ConnectorHandler func() Connector
	//Connector example:a user
	Connector interface {
		//SetConnection ...
		SetConnection(conn *Connection)
		Connection() *Connection
		//CheckAuth check the connector's Auth
		NewInstance() Connector
		CheckAuth(token string, wConn *websocket.Conn) error

		GetID() int64
	}
)
