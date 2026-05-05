package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd8100 struct {
	SerialNumber uint16 `json:"serial_number"`
	Result       uint8  `json:"result"`
	AuthCode     string `json:"auth_code"`
}

func NewCmd8100(serialNumber uint16, result uint8, authCode string) *Cmd8100 {
	return &Cmd8100{
		SerialNumber: serialNumber,
		Result:       result,
		AuthCode:     authCode,
	}
}

func (o *Cmd8100) GetMessageID() uint16 {
	return 0x8100
}

func (o *Cmd8100) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint16(b, o.SerialNumber)
	b = append(b, o.Result)
	b = append(b, []byte(o.AuthCode)...)
	return b, len(b)
}

func ParseCmd8100(b []byte) (*Cmd8100, error) {
	if len(b) < 3 {
		return nil, ErrBufferTooShort
	}
	serialNumber, _, _ := beutils.ReadU16(b, 0)
	result, _, _ := beutils.ReadU8(b, 2)
	authCode, _, _ := beutils.ReadByteSlice(b, 3, len(b)-3)
	return &Cmd8100{
		SerialNumber: serialNumber,
		Result:       result,
		AuthCode:     string(authCode),
	}, nil
}
