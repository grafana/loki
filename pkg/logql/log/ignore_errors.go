package log

import (
	"github.com/grafana/loki/pkg/logqlmodel"
)

type IgnoreErrors struct {
	filter *StringLabelFilter
}

func NewIgnoreErrors(filter *StringLabelFilter) *IgnoreErrors {
	return &IgnoreErrors{filter: filter}
}

func (ie *IgnoreErrors) Process(ts int64, line []byte, lbls *LabelsBuilder) ([]byte, bool) {
	if ie.filter == nil {
		lbls.ResetError()
		lbls.ResetErrorDetails()
		return line, true
	}

	if ie.filter.Name != logqlmodel.ErrorLabel {
		return line, true
	}

	_, valid := ie.filter.Process(ts, line, lbls)
	if valid {
		lbls.ResetError()
		lbls.ResetErrorDetails()
	}
	return line, true
}

func (ie *IgnoreErrors) RequiredLabelNames() []string { return []string{} }
