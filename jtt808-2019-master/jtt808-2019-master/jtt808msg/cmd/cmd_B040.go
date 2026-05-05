package cmd

type CmdB040 struct {
}

func NewCmdB040() *CmdB040 {
	return &CmdB040{}
}

func (o *CmdB040) GetMessageID() uint16 {
	return 0xB040
}

func (o *CmdB040) Write(b []byte) ([]byte, int) {
	return b, 0
}

func ParseCmdB040(b []byte) (*CmdB040, error) {
	return &CmdB040{}, nil
}
