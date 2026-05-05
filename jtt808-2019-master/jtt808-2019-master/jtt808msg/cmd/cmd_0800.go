package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd0800 struct {
	MultiMediaDataID uint32 `json:"multimedia_data_id"`
	MultiMediaType   uint8  `json:"multimedia_type"`
	MultiMediaFormat uint8  `json:"multimedia_format"`
	EventCoding      uint8  `json:"event_coding"`
	ChannleID        uint8  `json:"channel_id"`
}

func NewCmd0800(multimediaDataId uint32, multimediaType uint8, multimediaFormat uint8, eventCoding uint8, channelId uint8) *Cmd0800 {
	return &Cmd0800{
		MultiMediaDataID: multimediaDataId,
		MultiMediaType:   multimediaType,
		MultiMediaFormat: multimediaFormat,
		EventCoding:      eventCoding,
		ChannleID:        channelId,
	}
}

func (o *Cmd0800) GetMessageID() uint16 {
	return 0x0800
}

func (o *Cmd0800) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint32(b, o.MultiMediaDataID)
	b = append(b, o.MultiMediaType)
	b = append(b, o.MultiMediaFormat)
	b = append(b, o.EventCoding)
	b = append(b, o.ChannleID)
	return b, len(b)
}

func ParseCmd0800(b []byte) (*Cmd0800, error) {
	if len(b) < 8 {
		return nil, ErrBufferTooShort
	}
	multimediaDataId, _, _ := beutils.ReadU32(b, 0)
	multimediaType, _, _ := beutils.ReadU8(b, 4)
	multimediaFormat, _, _ := beutils.ReadU8(b, 5)
	eventCoding, _, _ := beutils.ReadU8(b, 6)
	channelId, _, _ := beutils.ReadU8(b, 7)
	return &Cmd0800{
		MultiMediaDataID: multimediaDataId,
		MultiMediaType:   multimediaType,
		MultiMediaFormat: multimediaFormat,
		EventCoding:      eventCoding,
		ChannleID:        channelId,
	}, nil
}
