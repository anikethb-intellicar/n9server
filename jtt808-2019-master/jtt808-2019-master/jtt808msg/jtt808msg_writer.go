package jtt808msg

import (
	"github.com/fabrikiot/jtt808-2019/jtt808msg/cmd"
)

func WriteJTT808Msg(cmd cmd.JTT808Cmd, inHeader *JTT808MsgHeader) []byte {
	bodyBytes, _ := cmd.Write(make([]byte, 0, 1024))

	properties := inHeader.Properties
	properties = (properties & 0x1C00) | uint16(len(bodyBytes)&0x03FF)

	// Prepare the output header..
	header := JTT808MsgHeader{
		MessageID:       cmd.GetMessageID(),
		Properties:      properties,
		ProtocolVersion: inHeader.ProtocolVersion,
		PhoneNumber:     inHeader.PhoneNumber,
		SerialNumber:    inHeader.SerialNumber,
		PackageInfo:     inHeader.PackageInfo,
	}

	headerBytes, _ := header.Write(make([]byte, 0, 1024))

	rawBeacon := append(headerBytes, bodyBytes...)
	rawBeacon = append(rawBeacon, JTT808UtilsCalculateChecksum(rawBeacon))
	finalBeacon := make([]byte, 0, len(rawBeacon)+2)
	finalBeacon = append(finalBeacon, 0x7E)
	finalBeacon = append(finalBeacon, rawBeacon...)
	finalBeacon = append(finalBeacon, 0x7E)
	return finalBeacon
}
