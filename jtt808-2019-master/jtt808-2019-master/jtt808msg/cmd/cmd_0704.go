package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type LocationReportDataItem struct {
	DataBodyLength uint16   `json:"data_body_length"`
	DataBody       *Cmd0200 `json:"data_body"`
}

type Cmd0704 struct {
	NumberOfDataItems      uint16                  `json:"number_of_data_items"`
	TypeOfData             uint8                   `json:"type_of_data"`
	LocationReportDataItem *LocationReportDataItem `json:"location_report_data_item"`
}

func NewCmd0704(numberOfDataItems uint16, typeOfData uint8, locationReportDataItem *LocationReportDataItem) *Cmd0704 {
	return &Cmd0704{
		NumberOfDataItems:      numberOfDataItems,
		TypeOfData:             typeOfData,
		LocationReportDataItem: locationReportDataItem,
	}
}

func (o *Cmd0704) Write(b []byte) ([]byte, int) {
	b = binary.BigEndian.AppendUint16(b, o.NumberOfDataItems)
	b = append(b, o.TypeOfData)
	b, _ = o.LocationReportDataItem.DataBody.Write(b)
	return b, len(b)

}

func (o *Cmd0704) GetMessageID() uint16 {
	return 0x0704
}

func ParseCmd0704(b []byte) (*Cmd0704, error) {
	if len(b) < 4 {
		return nil, ErrBufferTooShort
	}
	numberOfDataItems, _, _ := beutils.ReadU16(b, 0)
	typeOfData, _, _ := beutils.ReadU8(b, 2)
	dataBodyLength, _, _ := beutils.ReadU16(b, 3)
	dataBody, _, _ := beutils.ReadByteSlice(b, 5, len(b)-5)
	cmd0200, err := ParseCmd0200(dataBody)
	if err != nil {
		return nil, err
	}
	return &Cmd0704{
		NumberOfDataItems: numberOfDataItems,
		TypeOfData:        typeOfData,
		LocationReportDataItem: &LocationReportDataItem{
			DataBodyLength: dataBodyLength,
			DataBody:       cmd0200,
		},
	}, nil
}
