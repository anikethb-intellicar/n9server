package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd9207 struct {
	ResponseSerialNumber uint16 `json:"response_serial_number"`
	UploadControl        uint8  `json:"upload_control"`
}

func NewCmd9207(responseSerialNumber uint16, uploadControl uint8) *Cmd9207 {
	return &Cmd9207{
		ResponseSerialNumber: responseSerialNumber,
		UploadControl:        uploadControl,
	}
}

func (o *Cmd9207) GetMessageID() uint16 {
	return 0x9207
}

func (o *Cmd9207) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint16(b, o.ResponseSerialNumber)
	b = append(b, o.UploadControl)
	return b, len(b)
}

func ParseCmd9207(b []byte) (*Cmd9207, error) {
	if len(b) < 3 {
		return nil, ErrBufferTooShort
	}
	responseSerialNumber, _, _ := beutils.ReadU16(b, 0)
	uploadControl, _, _ := beutils.ReadU8(b, 2)
	return &Cmd9207{
		ResponseSerialNumber: responseSerialNumber,
		UploadControl:        uploadControl,
	}, nil
}
