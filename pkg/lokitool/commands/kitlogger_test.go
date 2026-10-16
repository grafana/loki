package commands

import (
	"bytes"
	"testing"

	kitlevel "github.com/go-kit/log/level"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestKitLogger(t *testing.T) {
	var out bytes.Buffer
	logger := &log.Logger{
		Out:       &out,
		Formatter: &log.TextFormatter{DisableColors: true, DisableTimestamp: true},
		Level:     log.DebugLevel,
	}
	l := kitLogger{logger: logger}

	for _, tc := range []struct {
		name string
		log  func() error
		want string
	}{
		{
			name: "debug",
			log:  func() error { return kitlevel.Debug(l).Log("msg", "debug message", "key", "value") },
			want: `level=debug msg="debug message" key=value`,
		},
		{
			name: "info",
			log:  func() error { return kitlevel.Info(l).Log("msg", "info message", "count", 3) },
			want: `level=info msg="info message" count=3`,
		},
		{
			name: "warn",
			log:  func() error { return kitlevel.Warn(l).Log("msg", "warn message") },
			want: `level=warning msg="warn message"`,
		},
		{
			name: "error",
			log:  func() error { return kitlevel.Error(l).Log("msg", "error message", "err", "boom") },
			want: `level=error msg="error message" err=boom`,
		},
		{
			name: "without level",
			log:  func() error { return l.Log("msg", "message") },
			want: `level=info msg=message`,
		},
		{
			name: "odd number of values",
			log:  func() error { return l.Log("msg", "message", "key") },
			want: `level=info msg=message key="(MISSING)"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			require.NoError(t, tc.log())
			require.Equal(t, tc.want+"\n", out.String())
		})
	}
}
