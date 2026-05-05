package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd1206 struct {
	ResponseSerialNumber uint16 `json:"response_serial_number"`
	Result               uint8  `json:"result"`
}

func NewCmd1206(responseSerialNumber uint16, result uint8) *Cmd1206 {
	return &Cmd1206{
		ResponseSerialNumber: responseSerialNumber,
		Result:               result,
	}
}

func (o *Cmd1206) GetMessageID() uint16 {
	return 0x1206
}

func (o *Cmd1206) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint16(b, o.ResponseSerialNumber)
	b = append(b, o.Result)
	return b, len(b)
}

func ParseCmd1206(b []byte) (*Cmd1206, error) {
	if len(b) < 3 {
		return nil, ErrBufferTooShort
	}
	responseSerialNumber, _, _ := beutils.ReadU16(b, 0)
	result, _, _ := beutils.ReadU8(b, 2)
	return &Cmd1206{
		ResponseSerialNumber: responseSerialNumber,
		Result:               result,
	}, nil
}
