package cmd

import "github.com/fabrikiot/goutils/beutils"

type Cmd0900 struct {
	MessageType    uint8  `json:"message_type"`
	Tranperentdata []byte `json:"tranperentdata"`
}

func NewCmd0900(messageType uint8, tranperentdata []byte) *Cmd0900 {
	return &Cmd0900{
		MessageType:    messageType,
		Tranperentdata: tranperentdata,
	}
}

func (o *Cmd0900) GetMessageID() uint16 {
	return 0x0900
}

func (o *Cmd0900) Write(b []byte) ([]byte, int) {
	b = append(b, o.MessageType)
	b = append(b, o.Tranperentdata...)
	return b, len(b)
}

func ParseCmd0900(b []byte) (*Cmd0900, error) {
	if len(b) < 2 {
		return nil, ErrBufferTooShort
	}
	messageType, _, _ := beutils.ReadU8(b, 0)
	tranperentdata, _, _ := beutils.ReadByteSlice(b, 1, len(b)-1)
	return &Cmd0900{
		MessageType:    messageType,
		Tranperentdata: tranperentdata,
	}, nil
}
