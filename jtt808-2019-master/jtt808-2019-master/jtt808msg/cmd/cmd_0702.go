package cmd

import "github.com/fabrikiot/goutils/beutils"

type Cmd0702 struct {
	Status                         uint8   `json:"status"`
	Time                           [6]byte `json:"time"`
	CardReadingResult              uint8   `json:"card_reading_result"`
	DriverNameLength               uint8   `json:"driver_name_length"`
	DriverName                     []byte  `json:"driver_name"`
	QualificationCertificationCode []byte  `json:"qualification_certification_code"`
	IssuingAuthorityNameLength     uint8   `json:"issuing_authority_name_length"`
	IssuingAgencyName              []byte  `json:"issuing_agency_name"`
	CertificateValidity            [4]byte `json:"certificate_validity"`
	IdentificationNumber           []byte  `json:"identification_number"`
}

func NewCmd0702(status uint8, time [6]byte, cardReadingResult uint8, driverNameLength uint8, driverName []byte, qualificationCertificationCode []byte, issuingAuthorityNameLength uint8, issuingAgencyName []byte, certificateValidity [4]byte, identificationNumber []byte) *Cmd0702 {
	return &Cmd0702{
		Status:                         status,
		Time:                           time,
		CardReadingResult:              cardReadingResult,
		DriverNameLength:               driverNameLength,
		DriverName:                     driverName,
		QualificationCertificationCode: qualificationCertificationCode,
		IssuingAuthorityNameLength:     issuingAuthorityNameLength,
		IssuingAgencyName:              issuingAgencyName,
		CertificateValidity:            certificateValidity,
		IdentificationNumber:           identificationNumber,
	}
}

func (o *Cmd0702) GetMessageID() uint16 {
	return 0x0702
}

func (o *Cmd0702) Write(b []byte) ([]byte, int) {
	b = append(b, o.Status)
	b = append(b, o.Time[:]...)
	b = append(b, o.CardReadingResult)
	b = append(b, o.DriverNameLength)
	b = append(b, o.DriverName...)
	b = append(b, o.QualificationCertificationCode...)
	b = append(b, o.IssuingAuthorityNameLength)
	b = append(b, o.IssuingAgencyName...)
	b = append(b, o.CertificateValidity[:]...)
	b = append(b, o.IdentificationNumber...)
	return b, len(b)
}

func ParseCmd0702(b []byte) (*Cmd0702, error) {
	if len(b) < 37 {
		return nil, ErrBufferTooShort
	}
	status, _, _ := beutils.ReadU8(b, 0)
	time, _, _ := beutils.ReadByteSlice(b, 1, 6)
	cardReadingResult, _, _ := beutils.ReadU8(b, 7)
	driverNameLength, _, _ := beutils.ReadU8(b, 8)
	driverName, _, _ := beutils.ReadByteSlice(b, 9, int(driverNameLength))
	qualificationCertificationCode, _, _ := beutils.ReadByteSlice(b, 9+int(driverNameLength), 20)
	issuingAuthorityNameLength, _, _ := beutils.ReadU8(b, 29+int(driverNameLength))
	issuingAgencyName, _, _ := beutils.ReadByteSlice(b, 30+int(driverNameLength), int(issuingAuthorityNameLength))
	certificateValidity, _, _ := beutils.ReadByteSlice(b, 30+int(driverNameLength)+int(issuingAuthorityNameLength), 4)
	identificationNumber, _, _ := beutils.ReadByteSlice(b, 34+int(driverNameLength)+int(issuingAuthorityNameLength), 20)
	return &Cmd0702{
		Status:                         status,
		Time:                           [6]byte(time),
		CardReadingResult:              cardReadingResult,
		DriverNameLength:               driverNameLength,
		DriverName:                     driverName,
		QualificationCertificationCode: qualificationCertificationCode,
		IssuingAuthorityNameLength:     issuingAuthorityNameLength,
		IssuingAgencyName:              issuingAgencyName,
		CertificateValidity:            [4]byte(certificateValidity),
		IdentificationNumber:           identificationNumber,
	}, nil
}
