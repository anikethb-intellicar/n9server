package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type CmdB051 struct {
	ReplySerialNumber uint16 `json:"reply_serial_number"`
	ParameterLength   uint32 `json:"parameter_length"`
	StringParameter   uint8  `json:"string_parameter"`
}

func NewCmdB051(replySerialNumber uint16, parameterLength uint32, stringParameter uint8) *CmdB051 {
	return &CmdB051{
		ReplySerialNumber: replySerialNumber,
		ParameterLength:   parameterLength,
		StringParameter:   stringParameter,
	}
}

func (o *CmdB051) GetMessageID() uint16 {
	return 0xB051
}

func (o *CmdB051) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint16(b, o.ReplySerialNumber)
	b = binary.BigEndian.AppendUint32(b, o.ParameterLength)
	b = append(b, o.StringParameter)
	return b, len(b)
}

func ParseCmdB051(b []byte) (*CmdB051, error) {
	if len(b) < 8 {
		return nil, ErrBufferTooShort
	}
	replySerialNumber, _, _ := beutils.ReadU16(b, 0)
	parameterLength, _, _ := beutils.ReadU32(b, 2)
	stringParameter, _, _ := beutils.ReadU8(b, 6)

	return &CmdB051{
		ReplySerialNumber: replySerialNumber,
		ParameterLength:   parameterLength,
		StringParameter:   stringParameter,
	}, nil
}
