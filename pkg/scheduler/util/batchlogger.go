package util

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/grafana/dskit/services"
)

type BatchLogger struct {
	services.Service
	logger log.Logger
	msg    string
	mtx    sync.Mutex
	m      IntPointerMap
}

func NewBatchLogger(logger log.Logger, interval time.Duration, msg string) *BatchLogger {
	bl := &BatchLogger{
		logger: logger,
		msg:    msg,
	}
	bl.Service = services.NewTimerService(interval, nil, bl.log, nil).WithName("batch logger")
	return bl
}

func (bl *BatchLogger) log(ctx context.Context) error {
	bl.mtx.Lock()
	defer bl.mtx.Unlock()
	for k, v := range bl.m {
		bl.logger.Log("msg", bl.msg, "key", k, "count", *v)
		delete(bl.m, k)
	}
	return nil
}

func (bl *BatchLogger) Inc(key string) {
	bl.mtx.Lock()
	defer bl.mtx.Unlock()
	bl.m.Inc(key)
}
