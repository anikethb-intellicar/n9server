package cmd

import (
	"encoding/binary"
	"errors"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd9212 struct {
	FileNameLength          uint8                     `json:"file_name_length"`
	FileName                []byte                    `json:"file_name"`
	FileType                uint8                     `json:"file_type"`
	UploadResult            uint8                     `json:"upload_result"`
	NumSupplementaryPackets uint8                     `json:"num_supplementary_packets"`
	SupplementaryPackets    []SupplementaryDataPacket `json:"supplementary_packets"`
}

type SupplementaryDataPacket struct {
	DataOffset uint32 `json:"data_offset"`
	Length     uint32 `json:"length"`
}

func NewCmd9212(fileNameLength uint8, fileName []byte, fileType uint8, uploadResult uint8, supplementaryPackets []SupplementaryDataPacket) *Cmd9212 {
	return &Cmd9212{
		FileNameLength:          uint8(len(fileName)),
		FileName:                fileName,
		FileType:                fileType,
		UploadResult:            uploadResult,
		NumSupplementaryPackets: uint8(len(supplementaryPackets)),
		SupplementaryPackets:    supplementaryPackets,
	}
}

func (o *Cmd9212) GetMessageID() uint16 {
	return 0x9212
}

func (o *Cmd9212) Write(b []byte) ([]byte, int) {
	b = append(b, o.FileNameLength)
	b = append(b, o.FileName...)
	b = append(b, o.FileType)
	b = append(b, o.UploadResult)
	b = append(b, o.NumSupplementaryPackets)
	for _, packet := range o.SupplementaryPackets {
		b = binary.BigEndian.AppendUint32(b, packet.DataOffset)
		b = binary.BigEndian.AppendUint32(b, packet.Length)
	}
	return b, len(b)
}

func ParseCmd9212(b []byte) (*Cmd9212, error) {
	if len(b) < 4 { // Minimum: file_name_length(1) + file_type(1) + upload_result(1) + num_supplementary_packets(1)
		return nil, ErrBufferTooShort
	}

	offset := 0

	fileNameLength, newOffset, err := beutils.ReadU8(b, offset)
	if !err {
		return nil, errors.New("offset out of bounds")
	}
	offset = newOffset

	// Check if we have enough bytes for file name
	if len(b) < offset+int(fileNameLength)+3 { // +3 for file_type + upload_result + num_supplementary_packets
		return nil, ErrBufferTooShort
	}

	// Read file name
	fileName, newOffset, err := beutils.ReadByteSlice(b, offset, int(fileNameLength))
	if !err {
		return nil, errors.New("offset out of bounds")
	}
	offset = newOffset

	// Read file type
	fileType, newOffset, err := beutils.ReadU8(b, offset)
	if !err {
		return nil, errors.New("offset out of bounds")
	}
	offset = newOffset

	// Read upload result
	uploadResult, newOffset, err := beutils.ReadU8(b, offset)
	if !err {
		return nil, errors.New("offset out of bounds")
	}
	offset = newOffset

	// Read number of supplementary data packets
	numSupplementaryPackets, newOffset, err := beutils.ReadU8(b, offset)
	if !err {
		return nil, errors.New("offset out of bounds")
	}
	offset = newOffset

	// Check if we have enough bytes for all supplementary packets (8 bytes each)
	expectedLen := offset + int(numSupplementaryPackets)*8
	if len(b) < expectedLen {
		return nil, ErrBufferTooShort
	}

	// Read supplementary data packets
	supplementaryPackets := make([]SupplementaryDataPacket, numSupplementaryPackets)
	for i := 0; i < int(numSupplementaryPackets); i++ {
		dataOffset, newOffset, err := beutils.ReadU32(b, offset)
		if !err {
			return nil, errors.New("offset out of bounds")
		}
		offset = newOffset

		length, newOffset, err := beutils.ReadU32(b, offset)
		if !err {
			return nil, errors.New("offset out of bounds")
		}
		offset = newOffset

		supplementaryPackets[i] = SupplementaryDataPacket{
			DataOffset: dataOffset,
			Length:     length,
		}
	}

	return &Cmd9212{
		FileNameLength:          fileNameLength,
		FileName:                fileName,
		FileType:                fileType,
		UploadResult:            uploadResult,
		NumSupplementaryPackets: numSupplementaryPackets,
		SupplementaryPackets:    supplementaryPackets,
	}, nil
}
