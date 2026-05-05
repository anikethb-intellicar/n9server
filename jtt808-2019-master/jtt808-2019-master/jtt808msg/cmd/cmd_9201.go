package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd9201 struct {
	ServerIPLength  uint8  `json:"server_ip_length"`
	ServerIPAddress string `json:"server_ip_address"`
	TcpPort         uint16 `json:"tcp_port"`
	UDPPort         uint16 `json:"udp_port"`
	ChannelNumber   uint8  `json:"channel_number"`
	TypeOfData      uint8  `json:"type_of_data"`
	StreamType      uint8  `json:"stream_type"`
	MemoryType      uint8  `json:"memory_type"`
	PlaybackMethod  uint8  `json:"playback_method"`
	FastForward     uint8  `json:"fast_forward"`
	StartingTime    string `json:"starting_time"`
	EndTime         string `json:"end_time"`
}

func NewCmd9201(serverIPLength uint8, serverIPAddress string, tcpPort uint16, udpPort uint16, channelNumber uint8, typeOfData uint8, streamType uint8, memoryType uint8, playbackMethod uint8, fastForward uint8, startingTime string, endTime string) *Cmd9201 {
	return &Cmd9201{
		ServerIPLength:  serverIPLength,
		ServerIPAddress: serverIPAddress,
		TcpPort:         tcpPort,
		UDPPort:         udpPort,
		ChannelNumber:   channelNumber,
		TypeOfData:      typeOfData,
		StreamType:      streamType,
		MemoryType:      memoryType,
		PlaybackMethod:  playbackMethod,
		FastForward:     fastForward,
		StartingTime:    startingTime,
		EndTime:         endTime,
	}
}

func (o *Cmd9201) GetMessageID() uint16 {
	return 0x9201
}

func (o *Cmd9201) Write(b []byte) ([]byte, int) {
	b = append(b, o.ServerIPLength)
	b = append(b, o.ServerIPAddress...)
	b = binary.BigEndian.AppendUint16(b, o.TcpPort)
	b = binary.BigEndian.AppendUint16(b, o.UDPPort)
	b = append(b, o.ChannelNumber)
	b = append(b, o.TypeOfData)
	b = append(b, o.StreamType)
	b = append(b, o.MemoryType)
	b = append(b, o.PlaybackMethod)
	b = append(b, o.FastForward)
	b = append(b, o.StartingTime...)
	b = append(b, o.EndTime...)
	return b, len(b)
}

func ParseCmd9201(b []byte) (*Cmd9201, error) {
	if len(b) < 24 {
		return nil, ErrBufferTooShort
	}
	serverIPLength, _, _ := beutils.ReadU8(b, 0)
	serverIPAddress, _, _ := beutils.ReadByteSlice(b, 1, int(serverIPLength))
	tcpPort, _, _ := beutils.ReadU16(b, 1+int(serverIPLength))
	udpPort, _, _ := beutils.ReadU16(b, 1+int(serverIPLength)+2)
	channelNumber, _, _ := beutils.ReadU8(b, 1+int(serverIPLength)+4)
	typeOfData, _, _ := beutils.ReadU8(b, 1+int(serverIPLength)+5)
	streamType, _, _ := beutils.ReadU8(b, 1+int(serverIPLength)+6)
	memoryType, _, _ := beutils.ReadU8(b, 1+int(serverIPLength)+7)
	playbackMethod, _, _ := beutils.ReadU8(b, 1+int(serverIPLength)+8)
	fastForward, _, _ := beutils.ReadU8(b, 1+int(serverIPLength)+9)
	startingTime, _, _ := beutils.ReadByteSlice(b, 1+int(serverIPLength)+10, 6)
	endTime, _, _ := beutils.ReadByteSlice(b, 1+int(serverIPLength)+16, 6)
	return &Cmd9201{
		ServerIPLength:  serverIPLength,
		ServerIPAddress: string(serverIPAddress),
		TcpPort:         tcpPort,
		UDPPort:         udpPort,
		ChannelNumber:   channelNumber,
		TypeOfData:      typeOfData,
		StreamType:      streamType,
		MemoryType:      memoryType,
		PlaybackMethod:  playbackMethod,
		FastForward:     fastForward,
		StartingTime:    string(JTT808UtilsBCDToString(startingTime)),
		EndTime:         string(JTT808UtilsBCDToString(endTime)),
	}, nil
}
