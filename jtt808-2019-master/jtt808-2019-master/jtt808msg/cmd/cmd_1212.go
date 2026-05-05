package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd1212 struct {
	FileNameLength uint8  `json:"file_name_length"`
	FileName       []byte `json:"file_name"`
	FileType       uint8  `json:"file_type"`
	FileSize       uint32 `json:"file_size"`
}

func NewCmd1212(fileNameLength uint8, fileName []byte, fileType uint8, fileSize uint32) *Cmd1212 {
	return &Cmd1212{
		FileNameLength: fileNameLength,
		FileName:       fileName,
		FileType:       fileType,
		FileSize:       fileSize,
	}
}

func (o *Cmd1212) GetMessageID() uint16 {
	return 0x1212
}

func (o *Cmd1212) Write(b []byte) ([]byte, int) {
	b = append(b, o.FileNameLength)
	b = append(b, o.FileName...)
	b = append(b, o.FileType)
	b = binary.BigEndian.AppendUint32(b, o.FileSize)
	return b, len(b)
}

func ParseCmd1212(b []byte) (*Cmd1212, error) {
	fileNameLength, _, _ := beutils.ReadU8(b, 0)
	if len(b) < int(fileNameLength)+6 {
		return nil, ErrBufferTooShort
	}
	fileName, _, _ := beutils.ReadByteSlice(b, 1, int(fileNameLength))
	fileType, _, _ := beutils.ReadU8(b, 1+int(fileNameLength))
	fileSize, _, _ := beutils.ReadU32(b, 2+int(fileNameLength))
	return &Cmd1212{
		FileNameLength: fileNameLength,
		FileName:       fileName,
		FileType:       fileType,
		FileSize:       fileSize,
	}, nil
}
