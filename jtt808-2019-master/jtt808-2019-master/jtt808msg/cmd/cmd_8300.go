package cmd

import "github.com/fabrikiot/goutils/beutils"

type Cmd8300 struct {
	Sign     uint8  `json:"sign"`
	TextType uint8  `json:"text_type"`
	TestInfo []byte `json:"testinfo"`
}

func NewCmd8300(sign uint8, textType uint8, textinfo []byte) *Cmd8300 {
	return &Cmd8300{
		Sign:     sign,
		TextType: textType,
		TestInfo: textinfo,
	}
}

func (o *Cmd8300) GetMessageID() uint16 {
	return 0x8300
}

func (o *Cmd8300) Write(b []byte) ([]byte, int) {
	b = append(b, o.Sign)
	b = append(b, o.TextType)
	b = append(b, o.TestInfo...)
	return b, len(b)
}

func ParseCmd8300(b []byte) (*Cmd8300, error) {
	if len(b) < 3 {
		return nil, ErrBufferTooShort
	}
	sign, _, _ := beutils.ReadU8(b, 0)
	textType, _, _ := beutils.ReadU8(b, 1)
	textInfo, _, _ := beutils.ReadByteSlice(b, 2, len(b)-2)
	return &Cmd8300{
		Sign:     sign,
		TextType: textType,
		TestInfo: textInfo,
	}, nil
}
