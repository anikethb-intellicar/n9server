package cmd

import "errors"

var (
	ErrBufferTooShort = errors.New("buffer too short")
	ErrUnknownCmd     = errors.New("unknown command")
)

type JTT808Cmd interface {
	GetMessageID() uint16
	Write(b []byte) ([]byte, int)
}

func ParseCmd(msgId uint16, b []byte) (JTT808Cmd, error) {
	switch msgId {
	case 0x0001:
		return ParseCmd0001(b)
	case 0x0102:
		return ParseCmd0102(b)
	case 0x8001:
		return ParseCmd8001(b)
	case 0x0100:
		return ParseCmd0100(b)
	case 0x0002:
		return ParseCmd0002(b)
	case 0x0200:
		return ParseCmd0200(b)
	case 0x0704:
		return ParseCmd0704(b)
	case 0x0900:
		return ParseCmd0900(b)
	case 0x8900:
		return ParseCmd8900(b)
	case 0x8300:
		return ParseCmd8300(b)
	case 0x1005:
		return ParseCmd1005(b)
	case 0xB040:
		return ParseCmdB040(b)
	case 0x0800:
		return ParseCmd0800(b)
	case 0x0801:
		return ParseCmd0801(b)
	case 0x9102:
		return ParseCmd9102(b)
	case 0x9205:
		return ParseCmd9205(b)
	case 0x8100:
		return ParseCmd8100(b)
	case 0x8800:
		return ParseCmd8800(b)
	case 0x9101:
		return ParseCmd9101(b)
	case 0x9212:
		return ParseCmd9212(b)
	case 0x9206:
		return ParseCmd9206(b)
	case 0x9201:
		return ParseCmd9201(b)
	case 0x0702:
		return ParseCmd0702(b)
	case 0x1205:
		return ParseCmd1205(b)
	case 0x1206:
		return ParseCmd1206(b)
	case 0x1210:
		return ParseCmd1210(b)
	case 0x1211:
		return ParseCmd1211(b)
	case 0x1212:
		return ParseCmd1212(b)
	case 0x8105:
		return ParseCmd8105(b)
	case 0x9202:
		return ParseCmd9202(b)
	case 0x9207:
		return ParseCmd9207(b)
	case 0x9208:
		return ParseCmd9208(b)
	case 0xB050:
		return ParseCmdB050(b)
	case 0xB051:
		return ParseCmdB051(b)

	default:
		return nil, ErrUnknownCmd
	}
}
