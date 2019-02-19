package wslt

import (
	"github.com/gorilla/websocket"
	jsoniter "github.com/json-iterator/go"
)

var (
	json = jsoniter.ConfigFastest
)

type BusinessHandler func(*Context)

type (
	WsMessage struct {
		MessageType int `json:"type"`

		Data []byte `json:"data"`
	}
	BusinessMessage struct {
		StringType string `json:"type"`

		Data jsoniter.RawMessage `json:"data"`
	}
)

func (b *BusinessMessage) UnmarshalData(v interface{}) error {
	return json.Unmarshal(b.Data, v)
}

func NewBusinessMessage(stringType string, data interface{}) (msg *BusinessMessage, err error) {
	msg = &BusinessMessage{
		StringType: stringType,
	}
	var rawData []byte
	if rawData, err = json.Marshal(data); err != nil {
		return
	}
	msg.Data = rawData
	return
}

func CreateWsMessage(msgType string, data interface{}) (msg *WsMessage, err error) {
	var (
		rawData, wsMsgData []byte
	)
	if rawData, err = json.Marshal(data); err != nil {
		return
	}
	bMsg := &BusinessMessage{
		StringType: msgType,
		Data:       rawData,
	}
	if wsMsgData, err = json.Marshal(bMsg); err != nil {
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

func NewWsMessage(messageType int, date []byte) *WsMessage {
	return &WsMessage{
		MessageType: messageType,
		Data:        date,
	}
}
