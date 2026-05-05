package cmd

import (
	"encoding/binary"
	"errors"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd1005 struct {
	Starttime        string `json:"starttime"` // must be 6 bytes
	Endtime          string `json:"endtime"`   // must be 6 bytes
	NoofPeopleGetOn  uint16 `json:"noofpeoplegeton"`
	NoofPeopleGetOff uint16 `json:"noofpeoplegetoff"`
}

func NewCmd1005(starttime string, endtime string, noofpeoplegeton uint16, noofpeoplegetoff uint16) *Cmd1005 {
	return &Cmd1005{
		Starttime:        starttime,
		Endtime:          endtime,
		NoofPeopleGetOn:  noofpeoplegeton,
		NoofPeopleGetOff: noofpeoplegetoff,
	}
}

func (o *Cmd1005) GetMessageID() uint16 {
	return 0x1005
}

// Write always produces exactly 16 bytes
func (o *Cmd1005) Write(b []byte) ([]byte, int) {

	b = append(b, JTT808UtilsStringToBCD([]byte(o.Starttime))...)
	b = append(b, JTT808UtilsStringToBCD([]byte(o.Endtime))...)
	// append two uint16 fields
	b = binary.BigEndian.AppendUint16(b, o.NoofPeopleGetOn)
	b = binary.BigEndian.AppendUint16(b, o.NoofPeopleGetOff)

	return b, len(b)
}

// ParseCmd1005 expects exactly 16 bytes
func ParseCmd1005(b []byte) (*Cmd1005, error) {
	if len(b) < 16 {
		return nil, errors.New("buffer too short, must be 16 bytes")
	}

	starttime, _, _ := beutils.ReadByteSlice(b, 0, 6)
	endtime, _, _ := beutils.ReadByteSlice(b, 6, 6)
	noofpeoplegeton, _, _ := beutils.ReadU16(b, 12)
	noofpeoplegetoff, _, _ := beutils.ReadU16(b, 14)

	return &Cmd1005{
		Starttime:        string(JTT808UtilsBCDToString(starttime)),
		Endtime:          string(JTT808UtilsBCDToString(endtime)),
		NoofPeopleGetOn:  noofpeoplegeton,
		NoofPeopleGetOff: noofpeoplegetoff,
	}, nil
}
