// Copyright 2025 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build localvalidationscheme

package promlint

import (
	"io"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// Lint performs a linting pass, returning a slice of Problems indicating any
// issues found in the metrics stream. The slice is sorted by metric name
// and issue description.
func (l *Linter) Lint(scheme model.ValidationScheme) ([]Problem, error) {
	return l.lint(scheme)
}

func newDecoder(r io.Reader, format expfmt.Format, scheme model.ValidationScheme) (expfmt.Decoder, func()) {
	return expfmt.NewDecoder(r, format, scheme), func() {}

}
