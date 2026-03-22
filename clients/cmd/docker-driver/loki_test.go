package main

import (
	"testing"
	"time"

	"github.com/docker/docker/daemon/logger"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func Test_loki_LogWhenClosed(t *testing.T) {
	l, err := New(logger.Info{
		Config: map[string]string{
			"loki-url": "http://localhost:3000",
		},
	}, util_log.Logger)
	require.Nil(t, err)
	msg := logger.NewMessage()
	msg.Line = []byte(`foo`)
	msg.Timestamp = time.Now()
	require.Nil(t, l.Log(msg))
	require.Nil(t, l.Close())
	require.NotNil(t, l.Log(msg))
}
