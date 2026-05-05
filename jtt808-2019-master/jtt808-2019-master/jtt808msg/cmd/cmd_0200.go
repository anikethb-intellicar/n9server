package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd0200 struct {
	AlarmSign uint32  `json:"alarm_sign"`
	Condition uint32  `json:"condition"`
	Latitude  uint32  `json:"latitude"`
	Longitude uint32  `json:"longitude"`
	High      uint16  `json:"high"`
	Speed     uint16  `json:"speed"`
	Direction uint16  `json:"direction"`
	TimeBCD   [6]byte `json:"time_bcd"`
}

func NewCmd0200(alarmSign uint32, condition uint32, latitude uint32, longitude uint32, high uint16, speed uint16, direction uint16, timeBCD [6]byte) *Cmd0200 {
	return &Cmd0200{
		AlarmSign: alarmSign,
		Condition: condition,
		Latitude:  latitude,
		Longitude: longitude,
		High:      high,
		Speed:     speed,
		Direction: direction,
		TimeBCD:   timeBCD,
	}
}

func (o *Cmd0200) GetMessageID() uint16 {
	return 0x0200
}

func (o *Cmd0200) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint32(b, o.AlarmSign)
	b = binary.BigEndian.AppendUint32(b, o.Condition)
	b = binary.BigEndian.AppendUint32(b, o.Latitude)
	b = binary.BigEndian.AppendUint32(b, o.Longitude)
	b = binary.BigEndian.AppendUint16(b, o.High)
	b = binary.BigEndian.AppendUint16(b, o.Speed)
	b = binary.BigEndian.AppendUint16(b, o.Direction)
	b = append(b, o.TimeBCD[:]...)
	return b, 27
}

func ParseCmd0200(b []byte) (*Cmd0200, error) {
	if len(b) < 27 {
		return nil, ErrBufferTooShort
	}
	alarmSign, _, _ := beutils.ReadU32(b, 0)
	condition, _, _ := beutils.ReadU32(b, 4)
	latitude, _, _ := beutils.ReadU32(b, 8)
	longitude, _, _ := beutils.ReadU32(b, 12)
	high, _, _ := beutils.ReadU16(b, 16)
	speed, _, _ := beutils.ReadU16(b, 18)
	direction, _, _ := beutils.ReadU16(b, 20)
	timeBCD := [6]byte{}
	copy(timeBCD[:], b[22:28])
	return &Cmd0200{
		AlarmSign: alarmSign,
		Condition: condition,
		Latitude:  latitude,
		Longitude: longitude,
		High:      high,
		Speed:     speed,
		Direction: direction,
		TimeBCD:   timeBCD,
	}, nil
}
