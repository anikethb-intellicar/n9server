package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd9101 struct {
	ServerIPLength  uint8  `json:"server_ip_length"`
	ServerIPAddress string `json:"server_ip_address"`
	TcpPort         uint16 `json:"tcp_port"`
	UDPPort         uint16 `json:"udp_port"`
	ChannelNumber   uint8  `json:"channel_number"`
	TypeOfData      uint8  `json:"type_of_data"`
	StreamType      uint8  `json:"stream_type"`
}

func NewCmd9101(serverIPLength uint8, serverIPAddress string, tcpPort uint16, udpPort uint16, channelNumber uint8, typeOfData uint8, streamType uint8) *Cmd9101 {
	return &Cmd9101{
		ServerIPLength:  serverIPLength,
		ServerIPAddress: serverIPAddress,
		TcpPort:         tcpPort,
		UDPPort:         udpPort,
		ChannelNumber:   channelNumber,
		TypeOfData:      typeOfData,
		StreamType:      streamType,
	}
}

func (o *Cmd9101) GetMessageID() uint16 {
	return 0x9101
}

func (o *Cmd9101) Write(b []byte) ([]byte, int) {
	b = append(b, o.ServerIPLength)
	b = append(b, o.ServerIPAddress...)
	b = binary.BigEndian.AppendUint16(b, o.TcpPort)
	b = binary.BigEndian.AppendUint16(b, o.UDPPort)
	b = append(b, o.ChannelNumber)
	b = append(b, o.TypeOfData)
	b = append(b, o.StreamType)
	return b, len(b)
}

func ParseCmd9101(b []byte) (*Cmd9101, error) {
	if len(b) < 12 {
		return nil, ErrBufferTooShort
	}
	serverIPLength, _, _ := beutils.ReadU8(b, 0)
	ipAddrLen := int(serverIPLength)
	serverIPAddress, _, _ := beutils.ReadByteSlice(b, 1, ipAddrLen)
	tcpPort, _, _ := beutils.ReadU16(b, 1+ipAddrLen)
	udpPort, _, _ := beutils.ReadU16(b, 1+ipAddrLen+2)
	channelNumber, _, _ := beutils.ReadU8(b, 1+ipAddrLen+4)
	typeOfData, _, _ := beutils.ReadU8(b, 1+ipAddrLen+5)
	streamType, _, _ := beutils.ReadU8(b, 1+ipAddrLen+6)
	return &Cmd9101{
		ServerIPLength:  serverIPLength,
		ServerIPAddress: string(serverIPAddress),
		TcpPort:         tcpPort,
		UDPPort:         udpPort,
		ChannelNumber:   channelNumber,
		TypeOfData:      typeOfData,
		StreamType:      streamType,
	}, nil
}
