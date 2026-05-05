package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd0001 struct {
	SerialNumber uint16 `json:"serial_number"`
	ReplyID      uint16 `json:"reply_id"`
	Result       uint8  `json:"result"`
}

func NewCmd0001(serialNumber uint16, replyID uint16, result uint8) *Cmd0001 {
	return &Cmd0001{
		SerialNumber: serialNumber,
		ReplyID:      replyID,
		Result:       result,
	}
}

func (o *Cmd0001) GetMessageID() uint16 {
	return 0x0001
}

func (o *Cmd0001) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint16(b, o.SerialNumber)
	b = binary.BigEndian.AppendUint16(b, o.ReplyID)
	b = append(b, o.Result)
	return b, 5
}

func ParseCmd0001(b []byte) (*Cmd0001, error) {
	if len(b) < 5 {
		return nil, ErrBufferTooShort
	}
	serialNumber, _, _ := beutils.ReadU16(b, 0)
	replyID, _, _ := beutils.ReadU16(b, 2)
	result, _, _ := beutils.ReadU8(b, 4)
	return &Cmd0001{
		SerialNumber: serialNumber,
		ReplyID:      replyID,
		Result:       result,
	}, nil
}
