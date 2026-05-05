package cmd

import (
	"encoding/binary"

	"github.com/fabrikiot/goutils/beutils"
)

type Cmd9206 struct {
	ServerAdressLength      uint8    `json:"server_adress_length"`
	ServerAdress            string   `json:"server_adress"`
	Port                    uint16   `json:"port"`
	UserNameLength          uint8    `json:"user_name_length"`
	UserName                string   `json:"user_name"`
	PasswordLength          uint8    `json:"password_length"`
	Password                string   `json:"password"`
	FileUploadPathLength    uint8    `json:"file_upload_path_length"`
	FileUploadPath          string   `json:"file_upload_path"`
	LogicalChannelNumber    uint8    `json:"logical_channel_number"`
	Starttime               string   `json:"starttime"`
	Endtime                 string   `json:"endtime"`
	AlarmSign               [8]uint8 `json:"alarm_sign"`
	AudioVideoResouceType   uint8    `json:"audio_video_resouce_type"`
	StreamType              uint8    `json:"stream_type"`
	StorageLocation         uint8    `json:"storage_location"`
	TakeExecutionConditions uint8    `json:"take_execution_conditions"`
}

func NewCmd9206(serverAdressLength uint8, serverAdress string, port uint16, userNameLength uint8, userName string, passwordLength uint8, password string, fileUploadPathLength uint8, fileUploadPath string, logicalChannelNumber uint8, starttime string, endtime string, alarmSign [8]uint8, audioVideoResouceType uint8, streamType uint8, storageLocation uint8, takeExecutionConditions uint8) *Cmd9206 {
	return &Cmd9206{
		ServerAdressLength:      serverAdressLength,
		ServerAdress:            serverAdress,
		Port:                    port,
		UserNameLength:          userNameLength,
		UserName:                userName,
		PasswordLength:          passwordLength,
		Password:                password,
		FileUploadPathLength:    fileUploadPathLength,
		FileUploadPath:          fileUploadPath,
		LogicalChannelNumber:    logicalChannelNumber,
		Starttime:               starttime,
		Endtime:                 endtime,
		AlarmSign:               alarmSign,
		AudioVideoResouceType:   audioVideoResouceType,
		StreamType:              streamType,
		StorageLocation:         storageLocation,
		TakeExecutionConditions: takeExecutionConditions,
	}
}

func (o *Cmd9206) GetMessageID() uint16 {
	return 0x9206
}

func (o *Cmd9206) Write(b []byte) ([]byte, int) {
	b = append(b, o.ServerAdressLength)
	b = append(b, o.ServerAdress...)
	b = binary.BigEndian.AppendUint16(b, o.Port)
	b = append(b, o.UserNameLength)
	b = append(b, o.UserName...)
	b = append(b, o.PasswordLength)
	b = append(b, o.Password...)
	b = append(b, o.FileUploadPathLength)
	b = append(b, o.FileUploadPath...)
	b = append(b, o.LogicalChannelNumber)
	b = append(b, o.Starttime...)
	b = append(b, o.Endtime...)
	b = append(b, o.AlarmSign[:]...)
	b = append(b, o.AudioVideoResouceType)
	b = append(b, o.StreamType)
	b = append(b, o.StorageLocation)
	b = append(b, o.TakeExecutionConditions)
	return b, len(b)
}

func ParseCmd9206(b []byte) (*Cmd9206, error) {
	if len(b) < 31 {
		return nil, ErrBufferTooShort
	}

	serverAdressLength, _, _ := beutils.ReadU8(b, 0)
	klength := int(serverAdressLength)
	serverAdress, _, _ := beutils.ReadByteSlice(b, 1, klength)
	port, _, _ := beutils.ReadU16(b, 1+klength)
	userNameLength, _, _ := beutils.ReadU8(b, 1+klength+2)
	llenght := int(userNameLength)
	userName, _, _ := beutils.ReadByteSlice(b, 1+klength+3, llenght)

	passwordLength, _, _ := beutils.ReadU8(b, 1+klength+3+llenght)

	mlenght := int(passwordLength)

	password, _, _ := beutils.ReadByteSlice(b, 1+klength+3+llenght+1, mlenght)

	fileUploadPathLength, _, _ := beutils.ReadU8(b, 1+klength+3+llenght+1+mlenght)

	nlenght := int(fileUploadPathLength)

	fileUploadPath, _, _ := beutils.ReadByteSlice(b, 1+klength+3+llenght+1+mlenght+1, nlenght)
	logicalChannelNumber, _, _ := beutils.ReadU8(b, 1+klength+3+llenght+1+mlenght+1+nlenght)
	startingTime, _, _ := beutils.ReadByteSlice(b, 1+klength+3+llenght+1+mlenght+1+nlenght+1, 6)
	endTime, _, _ := beutils.ReadByteSlice(b, 1+klength+3+llenght+1+mlenght+1+nlenght+1+6, 6)
	alarmSign, _, _ := beutils.ReadByteSlice(b, 1+klength+3+llenght+1+mlenght+1+nlenght+1+6+6, 8)
	audioVideoResouceType, _, _ := beutils.ReadU8(b, 1+klength+3+llenght+1+mlenght+1+nlenght+1+6+6+8)
	streamType, _, _ := beutils.ReadU8(b, 1+klength+3+llenght+1+mlenght+1+nlenght+1+6+6+8+1)
	storageLocation, _, _ := beutils.ReadU8(b, 1+klength+3+llenght+1+mlenght+1+nlenght+1+6+6+8+1+1)
	takeExecutionConditions, _, _ := beutils.ReadU8(b, 1+klength+3+llenght+1+mlenght+1+nlenght+1+6+6+8+1+1+1)

	return &Cmd9206{
		ServerAdressLength:      serverAdressLength,
		ServerAdress:            string(serverAdress),
		Port:                    port,
		UserNameLength:          userNameLength,
		UserName:                string(userName),
		PasswordLength:          passwordLength,
		Password:                string(password),
		FileUploadPathLength:    fileUploadPathLength,
		FileUploadPath:          string(fileUploadPath),
		LogicalChannelNumber:    logicalChannelNumber,
		Starttime:               string(JTT808UtilsBCDToString(startingTime)),
		Endtime:                 string(JTT808UtilsBCDToString(endTime)),
		AlarmSign:               [8]uint8(alarmSign),
		AudioVideoResouceType:   audioVideoResouceType,
		StreamType:              streamType,
		StorageLocation:         storageLocation,
		TakeExecutionConditions: takeExecutionConditions,
	}, nil
}
