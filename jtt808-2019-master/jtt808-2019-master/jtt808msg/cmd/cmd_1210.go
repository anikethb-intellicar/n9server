package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type AlarmAttachmentMessage struct {
	FileNameLength uint8  `json:"file_name_length"`
	FileName       []byte `json:"file_name"`
	FileSize       uint32 `json:"file_size"`
}

type Cmd1210 struct {
	TerminationId             [7]byte                  `json:"termination_id"`
	AlarmIdentificationNumber [16]byte                 `json:"alarm_identification_number"`
	AlarmNumber               [32]byte                 `json:"alarm_number"`
	InformationType           uint8                    `json:"information_type"`
	NoOfAtttachments          uint8                    `json:"no_of_attachments"`
	AttachmentInformationList []AlarmAttachmentMessage `json:"attachment_information_list"`
}

func NewCmd1210(terminationId [7]byte, alarmIdentificationNumber [16]byte, alarmNumber [32]byte, informationType uint8, noOfAtttachments uint8, attachmentInformationList []AlarmAttachmentMessage) *Cmd1210 {
	return &Cmd1210{
		TerminationId:             terminationId,
		AlarmIdentificationNumber: alarmIdentificationNumber,
		AlarmNumber:               alarmNumber,
		InformationType:           informationType,
		NoOfAtttachments:          noOfAtttachments,
		AttachmentInformationList: attachmentInformationList,
	}
}

func (o *Cmd1210) GetMessageID() uint16 {
	return 0x1210
}

func (o *Cmd1210) Write(b []byte) ([]byte, int) {
	b = append(b, o.TerminationId[:]...)
	b = append(b, o.AlarmIdentificationNumber[:]...)
	b = append(b, o.AlarmNumber[:]...)
	b = append(b, o.InformationType)
	b = append(b, o.NoOfAtttachments)
	for _, attachmentInformation := range o.AttachmentInformationList {
		b = append(b, attachmentInformation.FileNameLength)
		b = append(b, attachmentInformation.FileName...)
		b = binary.BigEndian.AppendUint32(b, attachmentInformation.FileSize)
	}
	return b, len(b)
}

func ParseCmd1210(b []byte) (*Cmd1210, error) {
	if len(b) < 57 {
		return nil, ErrBufferTooShort
	}
	terminationId, _, _ := beutils.ReadByteSlice(b, 0, 7)
	alarmIdentificationNumber, _, _ := beutils.ReadByteSlice(b, 7, 16)
	alarmNumber, _, _ := beutils.ReadByteSlice(b, 23, 32)
	informationType, _, _ := beutils.ReadU8(b, 55)
	noOfAtttachments, _, _ := beutils.ReadU8(b, 56)

	startPos := 57
	attachmentInformationList := make([]AlarmAttachmentMessage, noOfAtttachments)
	for i := 0; i < int(noOfAtttachments); i++ {
		fileNameLength, _, _ := beutils.ReadU8(b, startPos)
		fileName, _, _ := beutils.ReadByteSlice(b, startPos+1, int(fileNameLength))
		fileSize, _, _ := beutils.ReadU32(b, startPos+1+int(fileNameLength))
		attachmentInformationList[i] = AlarmAttachmentMessage{
			FileNameLength: fileNameLength,
			FileName:       fileName,
			FileSize:       fileSize,
		}
		startPos += int(fileNameLength) + 5
	}
	return &Cmd1210{
		TerminationId:             [7]byte(terminationId),
		AlarmIdentificationNumber: [16]byte(alarmIdentificationNumber),
		AlarmNumber:               [32]byte(alarmNumber),
		InformationType:           informationType,
		NoOfAtttachments:          noOfAtttachments,
		AttachmentInformationList: attachmentInformationList,
	}, nil
}
