package jtt808msgserver

import (
	"encoding/json"
	"net"
	"time"
)

func (o *JTT808MsgServer) cleanupconnections() {
	for {
		o.activetcpconnlock.Lock()
		nactiveconn := len(o.activetcpconn)
		o.activetcpconnlock.Unlock()

		if nactiveconn == 0 {
			break
		}

		o.activetcpconnlock.Lock()
		for _, eachconn := range o.activetcpconn {
			eachconn.Stop()
		}
		o.activetcpconnlock.Unlock()
		time.Sleep(time.Millisecond * 1000)
	}
}

func (o *JTT808MsgServer) jtt808ConnHandler(tcpconn *net.TCPConn) {
	go func(tcpconn *net.TCPConn) {
		connid := tcpconn.RemoteAddr().String() + "->" + tcpconn.LocalAddr().String()
		jtt808ConnI := NewJTT808Conn(tcpconn, o, o.logger)

		o.activetcpconnlock.Lock()
		o.activetcpconn[connid] = jtt808ConnI
		o.activetcpconnlock.Unlock()

		jtt808ConnI.Run()

		o.activetcpconnlock.Lock()
		delete(o.activetcpconn, connid)
		o.activetcpconnlock.Unlock()
	}(tcpconn)
}

func (o *JTT808MsgServer) RegisterChannel(channelid string, connhdlr *JTT808Conn) {
	o.channelmaplock.Lock()
	defer o.channelmaplock.Unlock()
	oldconn, isok := o.channelmap[channelid]
	o.channelmap[channelid] = connhdlr
	if isok {
		oldconn.Stop()
	}
}

func (o *JTT808MsgServer) UnregisterChannel(channelid string, connhdlr *JTT808Conn) {
	o.channelmaplock.Lock()
	defer o.channelmaplock.Unlock()
	oldconn, isok := o.channelmap[channelid]
	if isok {
		if oldconn.GetConnID() == connhdlr.GetConnID() {
			delete(o.channelmap, channelid)
		}
	}
}

type AIS140KafkaMsg struct {
	DeviceID     string      `json:"deviceid"`
	IntTimestamp int64       `json:"inttimestamp"`
	ConnID       string      `json:"connid"`
	ConnectedAt  int64       `json:"connectedat"`
	MsgType      string      `json:"msgtype"`
	Data         interface{} `json:"data"`
}

func (o *JTT808MsgServer) LogBeaconCallback(key []byte, value []byte, offset int64, context interface{}, err error) {

}

func (o *JTT808MsgServer) LogBeaconToKafka(msgkey string, msg *AIS140KafkaMsg) {
	msgjsbytes, jserr := json.Marshal(msg)
	if jserr != nil {
		return
	}
	o.kwriter.Write([]byte(msgkey), msgjsbytes, nil, o.LogBeaconCallback)
}
