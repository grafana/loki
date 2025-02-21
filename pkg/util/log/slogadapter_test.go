// SPDX-License-Identifier: AGPL-3.0-only

package log

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/go-kit/log/level"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockLogger struct {
	mock.Mock
}

func (m *mockLogger) Log(keyvals ...interface{}) error {
	args := m.Called(keyvals...)
	return args.Error(0)
}

func TestSlogFromGoKit(t *testing.T) {
	levels := []level.Value{
		level.DebugValue(),
		level.InfoValue(),
		level.WarnValue(),
		level.ErrorValue(),
	}
	slogLevels := []slog.Level{
		slog.LevelDebug,
		slog.LevelInfo,
		slog.LevelWarn,
		slog.LevelError,
	}

	// Store original log level to restore after tests
	originalLevel := logLevel.String()
	t.Cleanup(func() {
		_ = logLevel.Set(originalLevel)
	})

	t.Run("enabled for the right slog levels when go-kit level configured", func(t *testing.T) {
		for i, l := range levels {
			switch i {
			case 0:
				require.NoError(t, logLevel.Set("debug"))
			case 1:
				require.NoError(t, logLevel.Set("info"))
			case 2:
				require.NoError(t, logLevel.Set("warn"))
			case 3:
				require.NoError(t, logLevel.Set("error"))
			default:
				panic(fmt.Errorf("unhandled level %d", i))
			}

			mLogger := &mockLogger{}
			logger := level.NewFilter(mLogger, logLevel.Option)
			slogger := SlogFromGoKit(logger)

			for j, sl := range slogLevels {
				if j >= i {
					assert.Truef(t, slogger.Enabled(context.Background(), sl), "slog logger should be enabled for go-kit level %v / slog level %v", l, sl)
				} else {
					assert.Falsef(t, slogger.Enabled(context.Background(), sl), "slog logger should not be enabled for go-kit level %v / slog level %v", l, sl)
				}
			}
		}
	})

	t.Run("wraps go-kit logger", func(_ *testing.T) {
		mLogger := &mockLogger{}
		slogger := SlogFromGoKit(mLogger)

		for _, l := range levels {
			mLogger.On("Log", level.Key(), l, "caller", mock.AnythingOfType("string"), "time", mock.AnythingOfType("time.Time"), "msg", "test", "attr", slog.StringValue("value")).Times(1).Return(nil)
			attrs := []any{"attr", "value"}
			switch l {
			case level.DebugValue():
				slogger.Debug("test", attrs...)
			case level.InfoValue():
				slogger.Info("test", attrs...)
			case level.WarnValue():
				slogger.Warn("test", attrs...)
			case level.ErrorValue():
				slogger.Error("test", attrs...)
			default:
				panic(fmt.Errorf("unrecognized level %v", l))
			}
		}
	})
}
