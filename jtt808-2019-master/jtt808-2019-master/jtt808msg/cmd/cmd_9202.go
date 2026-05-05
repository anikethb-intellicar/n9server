package cmd

import "github.com/fabrikiot/goutils/beutils"

type Cmd9202 struct {
	ChannelNumber                uint8  `json:"channel_number"`
	PlaybackControl              uint8  `json:"playback_control"`
	FastForwardOrRewindMultiples uint8  `json:"fast_forward_or_rewind_multiples"`
	DragPositionPlayback         string `json:"drag_position_playback"`
}

func NewCmd9202(channelNumber uint8, playbackControl uint8, fastForwardOrRewindMultiples uint8, dragPositionPlayback string) *Cmd9202 {
	return &Cmd9202{
		ChannelNumber:                channelNumber,
		PlaybackControl:              playbackControl,
		FastForwardOrRewindMultiples: fastForwardOrRewindMultiples,
		DragPositionPlayback:         dragPositionPlayback,
	}
}

func (o *Cmd9202) GetMessageID() uint16 {
	return 0x9202
}

func (o *Cmd9202) Write(b []byte) ([]byte, int) {
	b = append(b, o.ChannelNumber)
	b = append(b, o.PlaybackControl)
	b = append(b, o.FastForwardOrRewindMultiples)
	b = append(b, o.DragPositionPlayback...)
	return b, len(b)
}

func ParseCmd9202(b []byte) (*Cmd9202, error) {
	if len(b) < 4 {
		return nil, ErrBufferTooShort
	}
	channelNumber, _, _ := beutils.ReadU8(b, 0)
	playbackControl, _, _ := beutils.ReadU8(b, 1)
	fastForwardOrRewindMultiples, _, _ := beutils.ReadU8(b, 2)
	dragPositionPlayback, _, _ := beutils.ReadByteSlice(b, 3, 6)
	return &Cmd9202{
		ChannelNumber:                channelNumber,
		PlaybackControl:              playbackControl,
		FastForwardOrRewindMultiples: fastForwardOrRewindMultiples,
		DragPositionPlayback:         string(JTT808UtilsBCDToString(dragPositionPlayback)),
	}, nil
}
