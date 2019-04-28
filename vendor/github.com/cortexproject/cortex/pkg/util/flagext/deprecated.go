package flagext

import (
	"flag"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
)

type deprecatedFlag struct {
	name string
}

func (deprecatedFlag) String() string {
	return "deprecated"
}

func (d deprecatedFlag) Set(string) error {
	level.Warn(util.Logger).Log("msg", "flag disabled", "flag", d.name)
	return nil
}

// DeprecatedFlag logs a warning when you try to use it.
func DeprecatedFlag(f *flag.FlagSet, name, message string) {
	f.Var(deprecatedFlag{name}, name, message)
}
