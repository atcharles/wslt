package ws_rpc

import (
	"errors"
	"fmt"
	"time"

	jsoniter "github.com/json-iterator/go"

	"github.com/segmentio/ksuid"
)

func NewMessage(data []byte, conn Conn) *message {
	return &message{
		data:   data,
		client: conn,
	}
}

func NewCallMessage(conn Conn) *message {
	return &message{client: conn}
}

type (
	Map map[string]interface{}
)

type (
	Conn interface {
		SendMessage(tp string, data interface{}) (err error)
		AddCallHandler(rid string, handler CallHandler)
		RemoveHandler(rid string)
	}
	CallHandler interface {
		Call(tp string, args Map) (result []byte, err error)
		HasRead()
		SetMsg(msg *CallMsg)
	}

	message struct {
		data []byte
		msg  *CallMsg

		client   Conn
		doneChan chan struct{}
	}

	CallMsg struct {
		RequestID string

		Args   Map
		Result jsoniter.RawMessage
	}
)

func (m *message) SetMsg(msg *CallMsg) {
	m.msg = msg
}

func (m *message) Msg() *CallMsg {
	return m.msg
}

func (m *message) Done() <-chan struct{} {
	return m.doneChan
}

func (m *message) HasRead() {
	m.done()
}

func (m *message) Call(tp string, args Map) (result []byte, err error) {
	callMsg := &CallMsg{
		RequestID: "",
		Args:      args,
	}
	callMsg.generateRequestID()

	if err = m.client.SendMessage(tp, callMsg); err != nil {
		return
	}
	m.doneChan = make(chan struct{})
	m.client.AddCallHandler(callMsg.RequestID, m)

	tm := time.NewTimer(time.Second * 3)
	select {
	case <-m.Done():
		result = m.msg.Result
	case <-tm.C:
		err = fmt.Errorf("requestID:%s,args:%#v error:timeout\n", callMsg.RequestID, callMsg.Args)
		m.done()
	}
	tm.Stop()

	return
}

func (m *message) done() {
	close(m.doneChan)
	m.client.RemoveHandler(m.msg.RequestID)
}

func (m *message) UnmarshalCallMsg() (err error) {
	if !json.Valid(m.data) {
		return
	}
	m.msg = new(CallMsg)
	err = json.Unmarshal(m.data, m.msg)
	if err != nil {
		return
	}
	if len(m.msg.RequestID) == 0 {
		err = errors.New("未指定RequestID")
		return
	}
	return
}

func (m *CallMsg) generateRequestID() {
	ku := ksuid.New()
	m.RequestID = ku.String()
	m.Result, _ = json.Marshal("result")
}

func ValidCallMsg(data []byte) (msg *CallMsg, ok bool) {
	if !json.Valid(data) {
		return
	}
	msg = new(CallMsg)
	err := json.Unmarshal(data, msg)
	if err != nil {
		return
	}
	if len(msg.RequestID) == 0 {
		return
	}
	ok = true
	return
}
