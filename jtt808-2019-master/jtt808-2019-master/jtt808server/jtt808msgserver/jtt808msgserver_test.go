package jtt808msgserver_test

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fabrikiot/jtt808-2019/jtt808server/jtt808msglogger"
	"github.com/fabrikiot/jtt808-2019/jtt808server/jtt808msgserver"
)

// go test -timeout 99999s -v . -run ^TestJTT808MsgServer$ -count 1
func TestJTT808MsgServer(t *testing.T) {

	activethreads := &sync.WaitGroup{}
	logger := log.New(os.Stdout, "JTT808MsgServer:", log.LstdFlags|log.Lmicroseconds)
	klogger := log.New(os.Stdout, "KWRITER:", log.LstdFlags|log.Lmicroseconds)
	jtt808MsgServerStatecallback := &jtt808msgserver.JTT808MsgServerStateCallback{
		Started: func() {
			logger.Println("Server started")
		},
		Stopped: func() {
			logger.Println("Server stopped")
			activethreads.Done()
		},
	}

	kwriter := jtt808msglogger.NewJTT808MsgLogger(klogger)

	jtt808MsgServerI := jtt808msgserver.NewJTT808MsgServer(11000, kwriter, jtt808MsgServerStatecallback, logger)
	activethreads.Add(1)
	jtt808MsgServerI.Start()

	ossigch := make(chan os.Signal, 1)
	signal.Notify(ossigch, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	<-ossigch

	jtt808MsgServerI.Stop()
	activethreads.Wait()
	log.Println("Active threads is zero")
	time.Sleep(time.Millisecond * 1000)
	log.Println("Test done")
}
