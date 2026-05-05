package jtt808msgserver

import (
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fabrikiot/goutils/leutils"
	"github.com/fabrikiot/jtt808-2019/jtt808msg"
)

var (
	ErrStopped = errors.New("stopped")
)

type JTT808Conn struct {
	tcpconn         *net.TCPConn
	connid          string
	connectedat     int64
	jtt808MsgServer *JTT808MsgServer
	logger          *log.Logger

	msgch          chan *JTT808ConnTXMsg
	activereqcount *atomic.Int32
	stopped        *atomic.Uint32
}

type JTT808ConnTXMsg struct {
	ChannelID string
	Msg       []byte
}

func NewJTT808Conn(tcpconn *net.TCPConn, jtt808MsgServer *JTT808MsgServer, logger *log.Logger) *JTT808Conn {
	return &JTT808Conn{
		tcpconn:         tcpconn,
		connid:          "",
		connectedat:     0,
		jtt808MsgServer: jtt808MsgServer,
		logger:          logger,

		msgch:          make(chan *JTT808ConnTXMsg, 16),
		activereqcount: &atomic.Int32{},
		stopped:        &atomic.Uint32{},
	}
}

func (o *JTT808Conn) RunTX(datach chan *jtt808msg.JTT808Msg) {
	ticker := time.NewTicker(time.Millisecond * 1000)
	defer ticker.Stop()
forloop:
	for o.stopped.Load() == 0 {
		select {
		case msgtosend := <-o.msgch:
			o.logger.Println("Conn ID:", o.connid, "Sending msg:%v", msgtosend)
		case nextrxmsg := <-datach:
			err := o.cmdHandle(nextrxmsg)
			if err != nil {
				o.logger.Println("Conn ID:", o.connid, "Handle RX msg err:", err)
				break forloop
			}
		case <-ticker.C:
			continue
		}
	}
	closeerr := o.tcpconn.Close()
	if closeerr != nil {
		o.logger.Println("Conn ID:", o.connid, "Close err:", closeerr)
	}
	o.logger.Println("Conn ID:", o.connid, "TX Thread closed")
}

func (o *JTT808Conn) runRxInsertDataCh(datachA chan *jtt808msg.JTT808Msg, jtt808Msg *jtt808msg.JTT808Msg) error {
	o.logger.Printf("Conn ID:%s Inserting RX msg into datach, Header: %s, Body: %s", o.connid, leutils.ToHex(jtt808Msg.Header), leutils.ToHex(jtt808Msg.MessageBody))
	select {
	case datachA <- jtt808Msg:
		return nil
	default:
	}
	for o.stopped.Load() == 0 {
		select {
		case datachA <- jtt808Msg:
			return nil
		case <-time.After(time.Millisecond * 1000):
			continue
		}
	}
	return ErrStopped
}

func (o *JTT808Conn) RunRX(datachA chan *jtt808msg.JTT808Msg) {
	inprogbuf := make([]byte, 0, 2048)
	for o.stopped.Load() == 0 {
		readBuf := make([]byte, 2048)
		nBytesRead, readErr := o.tcpconn.Read(readBuf)
		if readErr != nil {
			o.logger.Println("Conn ID:", o.connid, "Read err:", readErr)
			break
		}
		if nBytesRead > 0 {
			inprogbuf = append(inprogbuf, readBuf[:nBytesRead]...)
		}
		for {
			o.logger.Printf("Conn ID:%s Reading next JTT808 Msg:%v", o.connid, leutils.ToHex(inprogbuf))
			nextValidPos, jtt808Msg := jtt808msg.GetNextJTT808Msg(inprogbuf, 0, len(inprogbuf))
			if jtt808Msg != nil {
				err := o.runRxInsertDataCh(datachA, jtt808Msg)
				if err != nil {
					o.logger.Println("Conn ID:", o.connid, "Insert data ch err:", err)
					break
				}
			}
			if nextValidPos == 0 {
				break
			}
			inprogbuf = inprogbuf[nextValidPos:]
		}
	}
	o.logger.Println("Conn ID:", o.connid, "Rx Thread closed")
}

func (o *JTT808Conn) Run() {
	o.connid = o.tcpconn.RemoteAddr().String() + "-" + o.tcpconn.LocalAddr().String()
	o.connectedat = time.Now().UnixMilli()
	o.logger.Println("Conn ID:", o.connid, "New connection")
	o.tcpconn.SetKeepAlive(true)
	// Most of the cloud network has 10 Min no activity timeout for TCP Connections...
	o.tcpconn.SetKeepAlivePeriod(time.Minute * 8)
	o.tcpconn.SetLinger(5)
	o.tcpconn.SetNoDelay(true)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	datach := make(chan *jtt808msg.JTT808Msg, 128)
	go func(datachA chan *jtt808msg.JTT808Msg) {
		o.RunTX(datachA)
		wg.Done()
	}(datach)
	o.RunRX(datach)
	o.Stop()
	for o.activereqcount.Load() > 0 {
		time.Sleep(time.Millisecond * 10)
	}
	close(o.msgch)
	wg.Wait()
	o.logger.Println("Conn ID:", o.connid, "Conn closed")
}

func (o *JTT808Conn) Send(nextmsg *JTT808ConnTXMsg) bool {
	if o.stopped.Load() != 0 {
		return false
	}
	o.activereqcount.Add(1)
	defer o.activereqcount.Add(-1)
	if o.stopped.Load() != 0 {
		return false
	}
	select {
	case o.msgch <- nextmsg:
		return true
	default:
		return false
	}
}

func (o *JTT808Conn) GetConnID() string {
	return o.connid
}

func (o *JTT808Conn) Stop() {
	o.logger.Println("Conn ID:", o.connid, "stop requested")
	o.stopped.Store(1)
}
