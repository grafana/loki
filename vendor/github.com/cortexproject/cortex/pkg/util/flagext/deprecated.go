package flagext

import (
	"flag"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

// DeprecatedFlagsUsed is the metric that counts deprecated flags set.
var DeprecatedFlagsUsed = promauto.NewCounter(
	prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "deprecated_flags_inuse_total",
		Help:      "The number of deprecated flags currently set.",
	})

type deprecatedFlag struct {
	name string
}

func (deprecatedFlag) String() string {
	return "deprecated"
}

func (d deprecatedFlag) Set(string) error {
	level.Warn(util_log.Logger).Log("msg", "flag disabled", "flag", d.name)
	DeprecatedFlagsUsed.Inc()
	return nil
}

// DeprecatedFlag logs a warning when you try to use it.
func DeprecatedFlag(f *flag.FlagSet, name, message string) {
	f.Var(deprecatedFlag{name}, name, message)
}
