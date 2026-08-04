package cfg

import (
	"errors"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	yaml "go.yaml.in/yaml/v4"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// unknownConfigTotal counts configuration fields and CLI flags that were not
// recognized while parsing with non-strict mode enabled. A nested unknown
// object is reported by the decoder as a single field and thus counts once.
var unknownConfigTotal = promauto.NewCounter(prometheus.CounterOpts{
	Namespace: constants.Loki,
	Name:      "unknown_config_total",
	Help:      "Total number of unknown configuration fields and CLI flags encountered during non-strict config parsing.",
})

// UnknownFields collects configuration fields and CLI flags that are not
// recognized during non-strict parsing. Reporting is deferred to the caller
// because the logger and metrics registry are not yet initialized while the
// config is parsed. In strict mode unknown fields fail fast with the decoder's
// native error instead of being collected here.
type UnknownFields struct {
	fields []string
}

func (u *UnknownFields) add(name string) {
	if u == nil {
		return
	}
	u.fields = append(u.fields, name)
}

// List returns the collected unknown field and flag names in encounter order.
func (u *UnknownFields) List() []string {
	if u == nil {
		return nil
	}
	return u.fields
}

// Len returns the number of collected unknown fields and flags.
func (u *UnknownFields) Len() int {
	if u == nil {
		return 0
	}
	return len(u.fields)
}

// Report logs every collected unknown field at WARN and increments the
// loki_unknown_config_total metric. Call once the logger is initialized and
// only when strict parsing is disabled.
func (u *UnknownFields) Report(logger log.Logger) {
	for _, f := range u.List() {
		level.Warn(logger).Log("msg", "ignoring unknown configuration option", "name", f)
		unknownConfigTotal.Inc()
	}
}

// collectYAML routes unknown-field decode errors to the collector and returns
// any remaining (real) decode errors so they still fail parsing.
func (u *UnknownFields) collectYAML(err error) error {
	var loadErrs *yaml.LoadErrors
	if !errors.As(err, &loadErrs) {
		return err
	}

	var remaining []*yaml.LoadError
	for _, le := range loadErrs.Errors {
		if name, ok := unknownFieldName(le.Message); ok {
			u.add(name)
			continue
		}
		remaining = append(remaining, le)
	}

	if len(remaining) > 0 {
		return &yaml.LoadErrors{Errors: remaining}
	}
	return nil
}

const (
	yamlUnknownFieldPrefix = "field "
	yamlUnknownFieldSuffix = " not found in type "
)

// unknownFieldName extracts the field name from a go-yaml "field X not found in
// type Y" decode error message, reported for keys absent from the target type.
func unknownFieldName(msg string) (string, bool) {
	if !strings.HasPrefix(msg, yamlUnknownFieldPrefix) {
		return "", false
	}
	idx := strings.Index(msg, yamlUnknownFieldSuffix)
	if idx < len(yamlUnknownFieldPrefix) {
		return "", false
	}
	return msg[len(yamlUnknownFieldPrefix):idx], true
}
