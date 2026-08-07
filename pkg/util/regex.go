package util //nolint:revive

import (
	"github.com/grafana/regexp/syntax"

	"github.com/grafana/loki/v3/pkg/util/syntaxutil"
)

// These helpers were moved to pkg/util/syntaxutil so that the logql packages can
// depend on them without pulling in the rest of pkg/util. They are re-exported
// here so existing callers of util.* keep working unchanged.

func IsCaseInsensitive(reg *syntax.Regexp) bool { return syntaxutil.IsCaseInsensitive(reg) }

func AllNonGreedy(regs ...*syntax.Regexp) { syntaxutil.AllNonGreedy(regs...) }

func ClearCapture(regs ...*syntax.Regexp) { syntaxutil.ClearCapture(regs...) }
