package jtt808msg

import (
	"errors"

	"github.com/fabrikiot/jtt808-2019/jtt808msg/cmd"
)

var (
	ErrHeaderTooShort = errors.New("header too short")
)

type JTT808Msg struct {
	Header      []byte
	MessageBody []byte
}

func NewJTT808Msg(header []byte, messageBody []byte) *JTT808Msg {
	return &JTT808Msg{
		Header:      header,
		MessageBody: messageBody,
	}
}

func (o *JTT808Msg) ParseMsgHeader() (*JTT808MsgHeader, error) {
	return ParseJTT808MsgHeader(o.Header)
}

func (o *JTT808Msg) Parse() (*JTT808MsgHeader, cmd.JTT808Cmd, error) {
	header, err := o.ParseMsgHeader()
	if err != nil {
		return nil, nil, err
	}
	cmd, err := cmd.ParseCmd(header.MessageID, o.MessageBody)
	if err != nil {
		return nil, nil, err
	}
	return header, cmd, nil
}
