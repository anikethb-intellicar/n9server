package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd9208 struct {
	AtttachmentServerIpAddressLength uint8    `json:"ip_address_length"`
	AttachmentServerIpAddress        []byte   `json:"ip_address"`
	AttachmentServerPortTcp          uint16   `json:"port_tcp"`
	AttachmentServerPortUdp          uint16   `json:"port_udp"`
	AlarmIdentificationNumber        [16]byte `json:"alarm_identification_number"`
	AlarmNumber                      [32]byte `json:"alarm_number"`
	Reserve                          [16]byte `json:"reserve"`
}

func NewCmd9208(atttachmentServerIpAddressLength uint8, attachmentServerIpAddress []byte, attachmentServerPortTcp uint16, attachmentServerPortUdp uint16, alarmIdentificationNumber [16]byte, alarmNumber [32]byte, reserve [16]byte) *Cmd9208 {
	return &Cmd9208{
		AtttachmentServerIpAddressLength: atttachmentServerIpAddressLength,
		AttachmentServerIpAddress:        attachmentServerIpAddress,
		AttachmentServerPortTcp:          attachmentServerPortTcp,
		AttachmentServerPortUdp:          attachmentServerPortUdp,
		AlarmIdentificationNumber:        alarmIdentificationNumber,
		AlarmNumber:                      alarmNumber,
		Reserve:                          reserve,
	}
}

func (o *Cmd9208) GetMessageID() uint16 {
	return 0x9208
}

func (o *Cmd9208) Write(b []byte) ([]byte, int) {
	b = append(b, o.AtttachmentServerIpAddressLength)
	b = append(b, o.AttachmentServerIpAddress...)
	b = binary.BigEndian.AppendUint16(b, o.AttachmentServerPortTcp)
	b = binary.BigEndian.AppendUint16(b, o.AttachmentServerPortUdp)
	b = append(b, o.AlarmIdentificationNumber[:]...)
	b = append(b, o.AlarmNumber[:]...)
	b = append(b, o.Reserve[:]...)
	return b, len(b)
}

func ParseCmd9208(b []byte) (*Cmd9208, error) {
	if len(b) < 69 {
		return nil, ErrBufferTooShort
	}
	atttachmentServerIpAddressLength, _, _ := beutils.ReadU8(b, 0)
	attachmentServerIpAddress, _, _ := beutils.ReadByteSlice(b, 1, int(atttachmentServerIpAddressLength))
	attachmentServerPortTcp, _, _ := beutils.ReadU16(b, 1+int(atttachmentServerIpAddressLength))
	attachmentServerPortUdp, _, _ := beutils.ReadU16(b, 3+int(atttachmentServerIpAddressLength))
	alarmIdentificationNumber, _, _ := beutils.ReadByteSlice(b, 5+int(atttachmentServerIpAddressLength), 16)
	alarmNumber, _, _ := beutils.ReadByteSlice(b, 21+int(atttachmentServerIpAddressLength), 32)
	reserve, _, _ := beutils.ReadByteSlice(b, 53+int(atttachmentServerIpAddressLength), 16)
	return &Cmd9208{
		AtttachmentServerIpAddressLength: atttachmentServerIpAddressLength,
		AttachmentServerIpAddress:        attachmentServerIpAddress,
		AttachmentServerPortTcp:          attachmentServerPortTcp,
		AttachmentServerPortUdp:          attachmentServerPortUdp,
		AlarmIdentificationNumber:        [16]byte(alarmIdentificationNumber),
		AlarmNumber:                      [32]byte(alarmNumber),
		Reserve:                          [16]byte(reserve),
	}, nil
}
