package fabtcpserver

import (
	"log"
	"net"
	"sync/atomic"
	"time"
)

type FabTCPServerStateCallback struct {
	Started     func()
	Stopped     func()
	ConnHandler func(tcpconn *net.TCPConn)
}

type FabTCPServer struct {
	port          int
	statecallback *FabTCPServerStateCallback
	logger        *log.Logger
	stopped       *atomic.Uint32 // 0 -> IDLE, 1 -> Running , 2 -> Stopped
}

func (o *FabTCPServer) listenerThread() {
	var listener *net.TCPListener = nil

	// 1. Iterate till we were able to start the listener...
	lastattemptat := int64(0)
	listenretrytimeout := int64(10000)
	for o.stopped.Load() == 1 {
		if lastattemptat+listenretrytimeout <= time.Now().UnixMilli() {
			lastattemptat = time.Now().UnixMilli()
			listenerL, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: nil, Port: o.port})
			if err == nil {
				listener = listenerL
				break
			}
			o.logger.Println("Failed to start the TCP Listener, Err:", err)
		}
		time.Sleep(time.Millisecond * 1000)
	}

	if listener == nil {
		// This will only happen if the stop has been called in between...
		return
	}

	for o.stopped.Load() == 1 {
		listener.SetDeadline(time.Now().Add(time.Millisecond * 1000))

		tcpconn, err := listener.AcceptTCP()

		if err != nil {
			if operr, isok := err.(*net.OpError); isok && operr.Timeout() {
				continue
			} else {
				// In case of error just stop the server....
				o.Stop()
				continue
			}
		}
		o.statecallback.ConnHandler(tcpconn)
	}
	listener.Close()
}

func (o *FabTCPServer) Start() bool {
	if !o.stopped.CompareAndSwap(0, 1) {
		return false
	}

	go func() {
		o.statecallback.Started()
		o.listenerThread()
		o.stopped.Store(0)
		o.statecallback.Stopped()
	}()

	return true
}

func (o *FabTCPServer) Stop() bool {
	return o.stopped.CompareAndSwap(1, 2)
}

func NewFabTCPServer(port int, statecallback *FabTCPServerStateCallback, logger *log.Logger) *FabTCPServer {
	return &FabTCPServer{
		port:          port,
		statecallback: statecallback,
		logger:        logger,
		stopped:       &atomic.Uint32{},
	}
}
