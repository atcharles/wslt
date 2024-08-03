package wslt

type ItfConnector interface {
	NewInstance() ItfConnector
	GetID() int64
	AuthorizationChecker() func(token string) bool
	SetConnection(conn *Connection)
	Connection() *Connection
}
