package cmd

type Cmd8105 struct {
}

func NewCmd8105() *Cmd8105 {
	return &Cmd8105{}
}

func (o *Cmd8105) GetMessageID() uint16 {
	return 0x8105
}

func (o *Cmd8105) Write(b []byte) ([]byte, int) {
	return b, 0
}

func ParseCmd8105(b []byte) (*Cmd8105, error) {
	return &Cmd8105{}, nil
}
