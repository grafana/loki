package constants

// Re-export from client module to maintain code reuse
import "github.com/grafana/loki/client/util"

const (
	Loki   = util.Loki
	Cortex = util.Cortex
	OTLP   = util.OTLP
)
