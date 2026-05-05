package jtt808msgserver

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fabrikiot/jtt808-2019/jtt808server/fabtcpserver"
	"github.com/fabrikiot/jtt808-2019/jtt808server/jtt808msglogger"
)

type JTT808MsgServerStateCallback struct {
	Started func()
	Stopped func()
}

type JTT808MsgServer struct {
	jtt808MsgPort int
	kwriter       *jtt808msglogger.JTT808MsgLogger
	statecallback *JTT808MsgServerStateCallback
	logger        *log.Logger

	// This is used by external code to send messages to a specific channel..
	// channelid (string) => can be the deviceid as well...
	channelmap     map[string]*JTT808Conn
	channelmaplock *sync.RWMutex

	// All the active connections which are currently connected to our tcp server will be present here...
	activetcpconn     map[string]*JTT808Conn
	activetcpconnlock *sync.RWMutex

	stopped *atomic.Uint32
}

func NewJTT808MsgServer(jtt808MsgPort int, kwriter *jtt808msglogger.JTT808MsgLogger, statecallback *JTT808MsgServerStateCallback, logger *log.Logger) *JTT808MsgServer {
	return &JTT808MsgServer{
		jtt808MsgPort: jtt808MsgPort,
		kwriter:       kwriter,
		statecallback: statecallback,
		logger:        logger,

		channelmap:     make(map[string]*JTT808Conn),
		channelmaplock: &sync.RWMutex{},

		activetcpconn:     make(map[string]*JTT808Conn),
		activetcpconnlock: &sync.RWMutex{},

		stopped: &atomic.Uint32{},
	}
}

func (o *JTT808MsgServer) Start() bool {
	if !o.stopped.CompareAndSwap(0, 1) {
		return false
	}

	activethreads := &sync.WaitGroup{}
	// 1. Start the AIS 140 server,
	ais140statecallback := &fabtcpserver.FabTCPServerStateCallback{
		Started: func() {
			o.logger.Println("AIS 140 Server started")
		},
		Stopped: func() {
			o.logger.Println("AIS 140 Server stopped")
			activethreads.Done()
		},
		ConnHandler: o.jtt808ConnHandler,
	}

	jtt808MsgTcpServerI := fabtcpserver.NewFabTCPServer(o.jtt808MsgPort, ais140statecallback, o.logger)
	activethreads.Add(1)
	isok := jtt808MsgTcpServerI.Start()
	if !isok {
		o.logger.Fatal("Failed to start the AIS 140 TCP Server")
	}

	// 3. Start the manager thread which will wait for the stop request and kill everyone...
	go func() {
		o.statecallback.Started()
		for o.stopped.Load() == 1 {
			time.Sleep(time.Millisecond * 1000)
		}
		jtt808MsgTcpServerI.Stop()
		activethreads.Wait()
		o.cleanupconnections()
		o.statecallback.Stopped()
	}()

	return true
}

func (o *JTT808MsgServer) Stop() bool {
	return o.stopped.CompareAndSwap(1, 2)
}

func (o *JTT808MsgServer) TX(channelid string, msg []byte) bool {
	o.channelmaplock.RLock()
	defer o.channelmaplock.RUnlock()
	connch, isok := o.channelmap[channelid]
	if !isok {
		return false
	}

	return connch.Send(&JTT808ConnTXMsg{
		ChannelID: channelid,
		Msg:       msg,
	})
}
