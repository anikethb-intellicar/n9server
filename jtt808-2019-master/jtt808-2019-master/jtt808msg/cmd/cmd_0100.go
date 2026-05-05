package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd0100 struct {
	ProvinceID     uint16   `json:"province_id"`
	CountryID      uint16   `json:"country_id"`
	ManufacturerID [11]byte `json:"manufacturer_id"`
	TerminalModel  [30]byte `json:"terminal_model"`
	TerminalID     [30]byte `json:"terminal_id"`
	LicenseColor   uint8    `json:"license_color"`
	LicensePlate   []byte   `json:"license_plate"`
}

func NewCmd0100(provinceID uint16, countryID uint16, manufacturerID [11]byte, terminalModel [30]byte, terminalID [30]byte, licenseColor uint8, licensePlate []byte) *Cmd0100 {
	return &Cmd0100{
		ProvinceID:     provinceID,
		CountryID:      countryID,
		ManufacturerID: [11]byte(manufacturerID),
		TerminalModel:  [30]byte(terminalModel),
		TerminalID:     [30]byte(terminalID),
		LicenseColor:   licenseColor,
		LicensePlate:   licensePlate,
	}
}

func (o *Cmd0100) GetMessageID() uint16 {
	return 0x0100
}

func (o *Cmd0100) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint16(b, o.ProvinceID)
	b = binary.BigEndian.AppendUint16(b, o.CountryID)
	b = append(b, o.ManufacturerID[:]...)
	b = append(b, o.TerminalModel[:]...)
	b = append(b, o.TerminalID[:]...)
	b = append(b, o.LicenseColor)
	b = append(b, o.LicensePlate...)
	return b, len(b)
}

func ParseCmd0100(b []byte) (*Cmd0100, error) {
	if len(b) < 76 {
		return nil, ErrBufferTooShort
	}
	provinceID, _, _ := beutils.ReadU16(b, 0)
	countryID, _, _ := beutils.ReadU16(b, 2)
	manufacturerID, _, _ := beutils.ReadByteSlice(b, 4, 11)
	terminalModel, _, _ := beutils.ReadByteSlice(b, 15, 30)
	terminalID, _, _ := beutils.ReadByteSlice(b, 45, 30)
	licenseColor, _, _ := beutils.ReadU8(b, 75)
	var licensePlate []byte = nil
	if len(b) > 76 {
		licensePlate, _, _ = beutils.ReadByteSlice(b, 76, len(b)-76)
	} else {
		licensePlate = []byte{}
	}
	return &Cmd0100{
		ProvinceID:     provinceID,
		CountryID:      countryID,
		ManufacturerID: [11]byte(manufacturerID),
		TerminalModel:  [30]byte(terminalModel),
		TerminalID:     [30]byte(terminalID),
		LicenseColor:   licenseColor,
		LicensePlate:   licensePlate,
	}, nil
}
