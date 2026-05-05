package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Resource struct {
	ChannelNumber uint8   `json:"channel_number"`
	StartingTime  string  `json:"starting_time"`
	EndTime       string  `json:"ending_time"`
	AlarmSign     [8]byte `json:"alarm_sign"`
	ResourceType  uint8   `json:"resource_type"`
	StreamType    uint8   `json:"stream_type"`
	MemoryType    uint8   `json:"memory_type"`
	FileSize      uint32  `json:"file_size"`
}

type Cmd1205 struct {
	SerialNumber   uint16     `json:"serial_number"`
	TotalResources uint32     `json:"total_resources"`
	ResourceList   []Resource `json:"resource_list"`
}

func NewCmd1205(serialNumber uint16, totalResources uint32, resourceList []Resource) *Cmd1205 {
	return &Cmd1205{
		SerialNumber:   serialNumber,
		TotalResources: totalResources,
		ResourceList:   resourceList,
	}
}

func (o *Cmd1205) GetMessageID() uint16 {
	return 0x1205
}

func (o *Cmd1205) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint16(b, o.SerialNumber)
	b = binary.BigEndian.AppendUint32(b, o.TotalResources)
	for _, resource := range o.ResourceList {
		b = append(b, resource.ChannelNumber)
		b = append(b, resource.StartingTime...)
		b = append(b, resource.EndTime...)
		b = append(b, resource.AlarmSign[:]...)
		b = append(b, resource.ResourceType)
		b = append(b, resource.StreamType)
		b = append(b, resource.MemoryType)
		b = binary.BigEndian.AppendUint32(b, resource.FileSize)
	}
	return b, len(b)
}

func ParseCmd1205(b []byte) (*Cmd1205, error) {
	if len(b) < 6 {
		return nil, ErrBufferTooShort
	}

	serialNumber, _, _ := beutils.ReadU16(b, 0)
	totalResources, _, _ := beutils.ReadU32(b, 2)

	startPos := 6
	resourceList := make([]Resource, totalResources)
	for i := 0; i < int(totalResources); i++ {
		channelNumber, _, _ := beutils.ReadU8(b, startPos)
		startingTime, _, _ := beutils.ReadByteSlice(b, startPos+1, 6)
		endTime, _, _ := beutils.ReadByteSlice(b, startPos+7, 6)
		alarmSign, _, _ := beutils.ReadByteSlice(b, startPos+13, 8)
		resourceType, _, _ := beutils.ReadU8(b, startPos+21)
		streamType, _, _ := beutils.ReadU8(b, startPos+22)
		memoryType, _, _ := beutils.ReadU8(b, startPos+23)
		fileSize, _, _ := beutils.ReadU32(b, startPos+24)
		resourceList[i] = Resource{
			ChannelNumber: channelNumber,
			StartingTime:  string(JTT808UtilsBCDToString(startingTime)),
			EndTime:       string(JTT808UtilsBCDToString(endTime)),
			AlarmSign:     [8]byte(alarmSign),
			ResourceType:  resourceType,
			StreamType:    streamType,
			MemoryType:    memoryType,
			FileSize:      fileSize,
		}
		startPos += 32
	}
	return &Cmd1205{
		SerialNumber:   serialNumber,
		TotalResources: totalResources,
		ResourceList:   resourceList,
	}, nil
}
