package ws_rpc

import (
	"errors"
	"fmt"
	"time"

	jsoniter "github.com/json-iterator/go"

	"github.com/segmentio/ksuid"
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

	Message struct {
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

func (m *Message) Call(tp string, args Map) (result []byte, err error) {
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

func (m *Message) Done() <-chan struct{} {
	return m.doneChan
}

func (m *Message) HasRead() {
	m.done()
}

func (m *Message) Msg() *CallMsg {
	return m.msg
}

func (m *Message) SetMsg(msg *CallMsg) {
	m.msg = msg
}

func (m *Message) UnmarshalCallMsg() (err error) {
	if !json.Valid(m.data) {
		return errors.New("invalid json")
	}
	m.msg = new(CallMsg)
	if err = json.Unmarshal(m.data, m.msg); err != nil {
		return
	}
	if len(m.msg.RequestID) == 0 {
		err = errors.New("unexpect requestID")
		return
	}
	return
}

func (m *Message) done() {
	close(m.doneChan)
	m.client.RemoveHandler(m.msg.RequestID)
}

func (m *CallMsg) generateRequestID() {
	ku := ksuid.New()
	m.RequestID = ku.String()
	m.Result, _ = json.Marshal("result")
}

type Map map[string]interface{}

func NewCallMessage(conn Conn) *Message {
	return &Message{client: conn}
}

func NewMessage(data []byte, conn Conn) *Message {
	return &Message{
		data:   data,
		client: conn,
	}
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
