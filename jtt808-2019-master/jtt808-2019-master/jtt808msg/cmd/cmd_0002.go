package cmd

type Cmd0002 struct {
}

func NewCmd0002() *Cmd0002 {
	return &Cmd0002{}
}

func (o *Cmd0002) GetMessageID() uint16 {
	return 0x0002
}

func (o *Cmd0002) Write(b []byte) ([]byte, int) {
	return b, 0
}

func ParseCmd0002(b []byte) (*Cmd0002, error) {
	return &Cmd0002{}, nil
}
