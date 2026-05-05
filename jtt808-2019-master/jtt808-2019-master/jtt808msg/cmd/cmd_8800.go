package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd8800 struct {
	MultiMediaDataID                  uint32 `json:"multimedia_data_id"`
	TotalNumberOfReTransmittedPackets uint8  `json:"total_number_of_re_transmitted_packets"`
	ListOfReTransmissionPacketsIds    []byte `json:"list_of_re_transmitted_packets_ids"`
}

func NewCmd8800(multimediaDataID uint32, totalNumberOfReTransmittedPackets uint8, listOfReTransmissionPacketsIds []byte) *Cmd8800 {
	return &Cmd8800{
		MultiMediaDataID:                  multimediaDataID,
		TotalNumberOfReTransmittedPackets: totalNumberOfReTransmittedPackets,
		ListOfReTransmissionPacketsIds:    listOfReTransmissionPacketsIds,
	}
}

func (o *Cmd8800) GetMessageID() uint16 {
	return 0x8800
}

func (o *Cmd8800) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint32(b, o.MultiMediaDataID)
	b = append(b, o.TotalNumberOfReTransmittedPackets)
	b = append(b, o.ListOfReTransmissionPacketsIds...)
	return b, len(b)
}

func ParseCmd8800(b []byte) (*Cmd8800, error) {
	if len(b) < 5 {
		return nil, ErrBufferTooShort
	}
	multimediaDataID, _, _ := beutils.ReadU32(b, 0)
	totalNumberOfReTransmittedPackets, _, _ := beutils.ReadU8(b, 4)
	listLen := int(totalNumberOfReTransmittedPackets) * 2
	listOfReTransmissionPacketsIds, _, _ := beutils.ReadByteSlice(b, 5, listLen)
	return &Cmd8800{
		MultiMediaDataID:                  multimediaDataID,
		TotalNumberOfReTransmittedPackets: totalNumberOfReTransmittedPackets,
		ListOfReTransmissionPacketsIds:    listOfReTransmissionPacketsIds,
	}, nil
}
