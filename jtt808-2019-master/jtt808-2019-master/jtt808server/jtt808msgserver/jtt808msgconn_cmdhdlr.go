package jtt808msgserver

import (
	"encoding/json"
	"fmt"

	"github.com/fabrikiot/goutils/leutils"
	"github.com/fabrikiot/jtt808-2019/jtt808msg"
	"github.com/fabrikiot/jtt808-2019/jtt808msg/cmd"
)

func (o *JTT808Conn) writeToConn(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	o.logger.Printf("Writing %d bytes to connection", len(b))
	o.logger.Printf("Data: %s", leutils.ToHex(b))

	bytesToWrite := len(b)
	bytesWritten := 0
	for bytesWritten < bytesToWrite {
		n, err := o.tcpconn.Write(b[bytesWritten:])
		if err != nil {
			return err
		}
		bytesWritten += n
	}
	return nil
}

func (o *JTT808Conn) toJson(v interface{}) string {
	json, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(json)
}

func (o *JTT808Conn) cmdHandle(JTT808Msg *jtt808msg.JTT808Msg) error {
	msgHeader, msgCmd, err := JTT808Msg.Parse()
	if err != nil {
		return err
	}
	o.logger.Printf("Conn ID:%s Msg Header: %v, Msg Cmd: %v", o.connid, o.toJson(msgHeader), o.toJson(msgCmd))
	switch msgHeader.MessageID {
	case 0x0100:
		return o.cmdHandle0100(msgHeader, msgCmd)
	case 0x0102:
		return o.cmdHandle0102(msgHeader, msgCmd)
	}
	return nil
}

func (o *JTT808Conn) cmdHandle0102(msgHeader *jtt808msg.JTT808MsgHeader, msgCmd cmd.JTT808Cmd) error {
	_, ok := msgCmd.(*cmd.Cmd0102)
	if !ok {
		return fmt.Errorf("invalid command")
	}

	responseCmd := cmd.NewCmd8001(msgHeader.SerialNumber, msgHeader.MessageID, 0)
	responseBytes := jtt808msg.WriteJTT808Msg(responseCmd, msgHeader)
	writeErr := o.writeToConn(responseBytes)
	if writeErr != nil {
		o.logger.Printf("Failed to send registration response: %v", writeErr)
		return writeErr
	}
	return nil
}

func (o *JTT808Conn) cmdHandle0100(msgHeader *jtt808msg.JTT808MsgHeader, msgCmd cmd.JTT808Cmd) error {
	_, ok := msgCmd.(*cmd.Cmd0100)
	if !ok {
		return fmt.Errorf("invalid command")
	}

	authCode := "AUTHCODE"
	responseCmd := cmd.NewCmd8100(msgHeader.SerialNumber, 0x00, authCode)

	responseBytes := jtt808msg.WriteJTT808Msg(responseCmd, msgHeader)
	writeErr := o.writeToConn(responseBytes)
	if writeErr != nil {
		o.logger.Printf("Failed to send registration response: %v", writeErr)
		return writeErr
	}
	return nil
}
