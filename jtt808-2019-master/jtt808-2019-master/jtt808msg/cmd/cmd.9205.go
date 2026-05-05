package cmd

import (
	"github.com/fabrikiot/goutils/beutils"
)

type Cmd9205 struct {
	ChannelNumber     uint8   `json:"channel_number"`
	StartingTimestamp string  `json:"starting_timestamp"`
	EndTime           string  `json:"end_time"`
	AlarmSign         [8]byte `json:"alarm_sign"`
	ResourceType      uint8   `json:"resource_type"`
	StreamType        uint8   `json:"stream_type"`
	MemoryType        uint8   `json:"memory_type"`
}

func NewCmd9205(channelNumber uint8, startingTimestamp string, endTime string, alarmSign [8]byte, resourceType uint8, streamType uint8, memoryType uint8) *Cmd9205 {
	return &Cmd9205{
		ChannelNumber:     channelNumber,
		StartingTimestamp: startingTimestamp,
		EndTime:           endTime,
		AlarmSign:         alarmSign,
		ResourceType:      resourceType,
		StreamType:        streamType,
		MemoryType:        memoryType,
	}
}

func (o *Cmd9205) GetMessageID() uint16 {
	return 0x9205
}

func (o *Cmd9205) Write(b []byte) ([]byte, int) {
	b = append(b, o.ChannelNumber)
	b = append(b, o.StartingTimestamp...)
	b = append(b, o.EndTime...)
	b = append(b, o.AlarmSign[:]...)
	b = append(b, o.ResourceType)
	b = append(b, o.StreamType)
	b = append(b, o.MemoryType)
	return b, len(b)
}

func ParseCmd9205(b []byte) (*Cmd9205, error) {
	if len(b) < 23 {
		return nil, ErrBufferTooShort
	}
	channelNumber, _, _ := beutils.ReadU8(b, 0)
	startingTimestamp, _, _ := beutils.ReadByteSlice(b, 1, 6)
	endTime, _, _ := beutils.ReadByteSlice(b, 7, 6)
	alarmSign, _, _ := beutils.ReadByteSlice(b, 13, 8)
	resourceType, _, _ := beutils.ReadU8(b, 21)
	streamType, _, _ := beutils.ReadU8(b, 22)
	memoryType, _, _ := beutils.ReadU8(b, 23)
	return &Cmd9205{
		ChannelNumber:     channelNumber,
		StartingTimestamp: string(JTT808UtilsBCDToString(startingTimestamp)),
		EndTime:           string(JTT808UtilsBCDToString(endTime)),
		AlarmSign:         [8]byte(alarmSign),
		ResourceType:      resourceType,
		StreamType:        streamType,
		MemoryType:        memoryType,
	}, nil
}
