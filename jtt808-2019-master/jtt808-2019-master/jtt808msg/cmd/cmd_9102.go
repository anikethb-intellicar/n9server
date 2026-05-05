package cmd

import "github.com/fabrikiot/goutils/beutils"

type Cmd9102 struct {
	ChannelNumber            uint8 `json:"channel_number"`
	ControlInstruction       uint8 `json:"control_instruction"`
	TurnOfaudioandVideoTypes uint8 `json:"turn_of_audio_and_video_type"`
	SwitchStream             uint8 `json:"switch_stream"`
}

func NewCmd9102(channelNumber uint8, controlInstruction uint8, turnOfaudioandVideoTypes uint8, switchStream uint8) *Cmd9102 {
	return &Cmd9102{
		ChannelNumber:            channelNumber,
		ControlInstruction:       controlInstruction,
		TurnOfaudioandVideoTypes: turnOfaudioandVideoTypes,
		SwitchStream:             switchStream,
	}
}

func (o *Cmd9102) GetMessageID() uint16 {
	return 0x9102
}

func (o *Cmd9102) Write(b []byte) ([]byte, int) {
	b = append(b, o.ChannelNumber)
	b = append(b, o.ControlInstruction)
	b = append(b, o.TurnOfaudioandVideoTypes)
	b = append(b, o.SwitchStream)
	return b, len(b)
}

func ParseCmd9102(b []byte) (*Cmd9102, error) {
	if len(b) < 3 {
		return nil, ErrBufferTooShort
	}
	channelNumber, _, _ := beutils.ReadU8(b, 0)
	controlInstruction, _, _ := beutils.ReadU8(b, 1)
	turnOfaudioandVideoTypes, _, _ := beutils.ReadU8(b, 2)
	switchStream, _, _ := beutils.ReadU8(b, 3)
	return &Cmd9102{
		ChannelNumber:            channelNumber,
		ControlInstruction:       controlInstruction,
		TurnOfaudioandVideoTypes: turnOfaudioandVideoTypes,
		SwitchStream:             switchStream,
	}, nil
}
