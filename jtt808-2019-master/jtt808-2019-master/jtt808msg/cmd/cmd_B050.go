package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type CmdB050 struct {
	MsgType         uint32 `json:"msg_type"`
	ParameterLength uint32 `json:"parameter_length"`
	StringParameter uint8  `json:"string_parameter"`
}

func NewCmdB050(msgType uint32, parameterLength uint32, stringParameter uint8) *CmdB050 {
	return &CmdB050{
		MsgType:         msgType,
		ParameterLength: parameterLength,
		StringParameter: stringParameter,
	}
}

func (o *CmdB050) GetMessageID() uint16 {
	return 0xB050
}

func (o *CmdB050) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint32(b, o.MsgType)
	b = binary.BigEndian.AppendUint32(b, o.ParameterLength)
	b = append(b, o.StringParameter)
	return b, len(b)
}

func ParseCmdB050(b []byte) (*CmdB050, error) {
	if len(b) < 8 {
		return nil, ErrBufferTooShort
	}
	msgType, _, _ := beutils.ReadU32(b, 0)
	parameterLength, _, _ := beutils.ReadU32(b, 4)
	stringParameter, _, _ := beutils.ReadU8(b, 8)
	return &CmdB050{
		MsgType:         msgType,
		ParameterLength: parameterLength,
		StringParameter: stringParameter,
	}, nil
}
