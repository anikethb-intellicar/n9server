package jtt808msg

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type PackageInfo struct {
	TotalPackets uint16 `json:"total_packets"`
	PacketIndex  uint16 `json:"packet_index"`
}

type JTT808MsgHeader struct {
	MessageID       uint16       `json:"message_id"`
	Properties      uint16       `json:"properties"`
	ProtocolVersion uint8        `json:"protocol_version"`
	PhoneNumber     string       `json:"phone_number"`
	SerialNumber    uint16       `json:"serial_number"`
	PackageInfo     *PackageInfo `json:"package_info"`
}

func (o *JTT808MsgHeader) Write(b []byte) ([]byte, error) {
	b = binary.BigEndian.AppendUint16(b, o.MessageID)
	b = binary.BigEndian.AppendUint16(b, o.Properties)
	b = append(b, o.ProtocolVersion)
	phoneNumberBCD := JTT808UtilsStringToBCD([]byte(o.PhoneNumber))
	// phoneNumberBCD should be 10 bytes, if it is smaller prepend 0x00 to make it 10 bytes
	for len(phoneNumberBCD) < 10 {
		phoneNumberBCD = append([]byte{0x00}, phoneNumberBCD...)
	}
	b = append(b, phoneNumberBCD...)
	b = binary.BigEndian.AppendUint16(b, o.SerialNumber)
	if o.PackageInfo != nil {
		b = binary.BigEndian.AppendUint16(b, o.PackageInfo.TotalPackets)
		b = binary.BigEndian.AppendUint16(b, o.PackageInfo.PacketIndex)
	}
	return b, nil
}

func ParseJTT808MsgHeader(b []byte) (*JTT808MsgHeader, error) {
	if len(b) < 17 {
		return nil, ErrHeaderTooShort

	}
	msgId, _, _ := beutils.ReadU16(b, 0)
	properties, _, _ := beutils.ReadU16(b, 2)
	protocolVersion, _, _ := beutils.ReadU8(b, 4)
	phoneNumber, _, _ := beutils.ReadByteSlice(b, 5, 10)
	serialNumber, _, _ := beutils.ReadU16(b, 15)

	var packageInfo *PackageInfo = nil
	if (properties & 0x2000) == 0x2000 {
		if len(b) < 21 {
			return nil, ErrHeaderTooShort
		}
		packageTotal, _, _ := beutils.ReadU16(b, 17)
		packageIndex, _, _ := beutils.ReadU16(b, 19)
		packageInfo = &PackageInfo{
			TotalPackets: packageTotal,
			PacketIndex:  packageIndex,
		}
	}

	header := &JTT808MsgHeader{
		MessageID:       msgId,
		Properties:      properties,
		ProtocolVersion: protocolVersion,
		PhoneNumber:     string(JTT808UtilsBCDToString(phoneNumber)),
		SerialNumber:    serialNumber,
		PackageInfo:     packageInfo,
	}

	return header, nil
}
