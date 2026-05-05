package jtt808msglogger

import "log"

type JTT808MsgLoggerWriteCallback func(key []byte, value []byte, offset int64, context interface{}, err error)

type JTT808MsgLogger struct {
	logger *log.Logger
}

func NewJTT808MsgLogger(logger *log.Logger) *JTT808MsgLogger {
	return &JTT808MsgLogger{logger: logger}
}

func (o *JTT808MsgLogger) Write(key []byte, value []byte, context interface{}, callback JTT808MsgLoggerWriteCallback) {
	o.logger.Printf("JTT808MsgLogger: %s, %s, %v", key, value, context)
	callback(key, value, 0, context, nil)
}
