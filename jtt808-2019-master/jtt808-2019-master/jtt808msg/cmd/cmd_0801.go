package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd0801 struct {
	MultiMediaDataID    uint32   `json:"multimedia_data_id"`
	MultiMediaType      uint8    `json:"multimedia_type"`
	MultiMediaFormat    uint8    `json:"multimedia_format"`
	EventCoding         uint8    `json:"event_coding"`
	ChannleID           uint8    `json:"channel_id"`
	LocationInformation [28]byte `json:"location_information"`
	MultimediaPacket    []byte   `json:"multimedia_packet"`
}

func NewCmd0801(multimediaDataId uint32, multimediaType uint8, multimediaFormat uint8, eventCoding uint8, channelId uint8, locationInformation [2]byte, multimediaPacket []byte) *Cmd0801 {
	return &Cmd0801{
		MultiMediaDataID: multimediaDataId,
		MultiMediaType:   multimediaType,
		MultiMediaFormat: multimediaFormat,
		EventCoding:      eventCoding,
		ChannleID:        channelId,
	}
}

func (o *Cmd0801) GetMessageID() uint16 {
	return 0x0801
}

func (o *Cmd0801) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint32(b, o.MultiMediaDataID)
	b = append(b, o.MultiMediaType)
	b = append(b, o.MultiMediaFormat)
	b = append(b, o.EventCoding)
	b = append(b, o.ChannleID)
	b = append(b, o.LocationInformation[:]...)
	b = append(b, o.MultimediaPacket...)
	return b, len(b)
}

func ParseCmd0801(b []byte) (*Cmd0801, error) {
	if len(b) < 36 {
		return nil, ErrBufferTooShort
	}
	multimediaDataId, _, _ := beutils.ReadU32(b, 0)
	multimediaType, _, _ := beutils.ReadU8(b, 4)
	multimediaFormat, _, _ := beutils.ReadU8(b, 5)
	eventCoding, _, _ := beutils.ReadU8(b, 6)
	channelId, _, _ := beutils.ReadU8(b, 7)
	locationInformation, _, _ := beutils.ReadByteSlice(b, 8, 28)
	multimediaPacket, _, _ := beutils.ReadByteSlice(b, 36, len(b)-36)
	return &Cmd0801{
		MultiMediaDataID:    multimediaDataId,
		MultiMediaType:      multimediaType,
		MultiMediaFormat:    multimediaFormat,
		EventCoding:         eventCoding,
		ChannleID:           channelId,
		LocationInformation: [28]byte(locationInformation),
		MultimediaPacket:    multimediaPacket,
	}, nil
}
