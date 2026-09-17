package memlimit

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

const (
	envAUTOMEMLIMIT_EXPERIMENT = "AUTOMEMLIMIT_EXPERIMENT"
)

// Experiments is a set of experiment flags.
// It is used to enable experimental features.
//
// You can set the flags by setting the environment variable AUTOMEMLIMIT_EXPERIMENT.
// The value of the environment variable is a comma-separated list of experiment names.
//
// The following experiment names are known:
//
//   - none: disable all experiments
//   - system: enable fallback to system memory limit
type Experiments struct {
	// System enables fallback to system memory limit.
	System bool
}

func parseExperiments() (Experiments, error) {
	var exp Experiments

	// Create a map of known experiment names.
	names := make(map[string]func(bool))
	rv := reflect.ValueOf(&exp).Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rv.Field(i)
		names[strings.ToLower(rt.Field(i).Name)] = field.SetBool
	}

	// Parse names.
	for _, f := range strings.Split(os.Getenv(envAUTOMEMLIMIT_EXPERIMENT), ",") {
		if f == "" {
			continue
		}
		if f == "none" {
			exp = Experiments{}
			continue
		}
		val := true
		set, ok := names[f]
		if !ok {
			return Experiments{}, fmt.Errorf("unknown AUTOMEMLIMIT_EXPERIMENT %s", f)
		}
		set(val)
	}

	return exp, nil
}
