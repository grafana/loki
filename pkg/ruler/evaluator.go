package ruler

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/logqlmodel"
)

// Evaluator is the interface that must be satisfied in order to accept rule evaluations from the Ruler.
type Evaluator interface {
	// Eval evaluates the given rule and returns the result.
	Eval(ctx context.Context, qs string, now time.Time) (*logqlmodel.Result, error)
}

type EvaluationConfig struct {
	Mode string `yaml:"mode,omitempty"`

	QueryFrontend QueryFrontendConfig `yaml:"query_frontend,omitempty"`
}

func (c *EvaluationConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.Mode, "ruler.evaluation.mode", EvalModeLocal, "The evaluation mode for the ruler. Can be either 'local' or 'remote'. If set to 'local', the ruler will evaluate rules locally. If set to 'remote', the ruler will evaluate rules remotely. If unset, the ruler will evaluate rules locally.")
	c.QueryFrontend.RegisterFlags(f)
}

func (c *EvaluationConfig) Validate() error {
	if c.Mode != EvalModeLocal && c.Mode != EvalModeRemote {
		return fmt.Errorf("invalid evaluation mode: %s. Acceptable modes are: %s", c.Mode, strings.Join([]string{EvalModeLocal, EvalModeRemote}, ", "))
	}

	return nil
}
