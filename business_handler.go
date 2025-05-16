package wslt

import (
	"github.com/gorilla/websocket"
	jsoniter "github.com/json-iterator/go"
)

type BusinessHandler func(*Context)

type (
	WsMessage struct {
		Type int    `json:"type,omitempty"`
		Data []byte `json:"data,omitempty"`
	}
	BusinessMessage struct {
		Type string              `json:"type,omitempty"`
		Data jsoniter.RawMessage `json:"data,omitempty"`
	}
)

func (b *BusinessMessage) UnmarshalData(v interface{}) error {
	return json.Unmarshal(b.Data, v)
}

func CreateWsMessage(msgType string, data interface{}) (msg *WsMessage, err error) {
	d, err := json.Marshal(data)
	if err != nil {
		return
	}
	bMsg := &BusinessMessage{Type: msgType, Data: d}
	wsMsgData, err := json.Marshal(bMsg)
	if err != nil {
		return
	}
	msg = NewWsMessage(websocket.TextMessage, wsMsgData)
	return
}

func DecodeBiMessage(data []byte) (msg *BusinessMessage, err error) {
	msg = new(BusinessMessage)
	err = json.Unmarshal(data, msg)
	return
}

func NewBusinessMessage(stringType string, data interface{}) (msg *BusinessMessage, err error) {
	d, err := json.Marshal(data)
	if err != nil {
		return
	}
	return &BusinessMessage{Type: stringType, Data: d}, nil
}

func NewWsMessage(messageType int, date []byte) *WsMessage {
	return &WsMessage{Type: messageType, Data: date}
}
