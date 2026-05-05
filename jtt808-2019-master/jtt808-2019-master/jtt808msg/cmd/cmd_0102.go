package cmd

import (
	"github.com/fabrikiot/goutils/beutils"
)

type Cmd0102 struct {
	AuthcodeLength        uint8    `json:"authcodelength"`
	AuthCodeContent       []byte   `json:"authcodecontent"`
	TerminalIMEI          [15]byte `json:"terminalimei"`
	FirmwareVersionNumber [20]byte `json:"firmwareversionnumber"`
}

func NewCmd0102(authcodeLength uint8, authCodeContent []byte, terminalIMEI [15]byte, firmwareVersionNumber [20]byte) *Cmd0102 {
	return &Cmd0102{
		AuthcodeLength:        authcodeLength,
		AuthCodeContent:       authCodeContent,
		TerminalIMEI:          terminalIMEI,
		FirmwareVersionNumber: firmwareVersionNumber,
	}
}

func (o *Cmd0102) GetMessageID() uint16 {
	return 0x0102
}

func (o *Cmd0102) Write(b []byte) ([]byte, int) {
	b = append(b, o.AuthcodeLength)
	b = append(b, o.AuthCodeContent...)
	b = append(b, o.TerminalIMEI[:]...)
	b = append(b, o.FirmwareVersionNumber[:]...)
	return b, len(b)
}

func ParseCmd0102(b []byte) (*Cmd0102, error) {
	if len(b) < 36 {
		return nil, ErrBufferTooShort
	}
	authCodeLength, currIdx, _ := beutils.ReadU8(b, 0)
	authCodeContent, currIdx, _ := beutils.ReadByteSlice(b, currIdx, int(authCodeLength))
	terminalIMEI, currIdx, _ := beutils.ReadByteSlice(b, currIdx, 15)
	firmwareVersionNumber, _, _ := beutils.ReadByteSlice(b, currIdx, 20)
	return &Cmd0102{
		AuthcodeLength:        authCodeLength,
		AuthCodeContent:       authCodeContent,
		TerminalIMEI:          [15]byte(terminalIMEI),
		FirmwareVersionNumber: [20]byte(firmwareVersionNumber),
	}, nil
}
