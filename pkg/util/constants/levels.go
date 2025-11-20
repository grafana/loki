package constants

// Re-export from client module to maintain code reuse
import "github.com/grafana/loki/client/util"

const (
	LevelLabel       = util.LevelLabel
	LogLevelUnknown  = util.LogLevelUnknown
	LogLevelDebug    = util.LogLevelDebug
	LogLevelInfo     = util.LogLevelInfo
	LogLevelWarn     = util.LogLevelWarn
	LogLevelError    = util.LogLevelError
	LogLevelFatal    = util.LogLevelFatal
	LogLevelCritical = util.LogLevelCritical
	LogLevelTrace    = util.LogLevelTrace
)

var LogLevels = util.LogLevels
