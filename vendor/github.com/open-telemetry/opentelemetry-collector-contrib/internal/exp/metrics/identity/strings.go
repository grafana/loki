// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package identity // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"

import (
	"fmt"
)

func (r Resource) String() string {
	return fmt.Sprintf("resource/%x", r.Hash().Sum64())
}

func (s Scope) String() string {
	return fmt.Sprintf("scope/%x", s.Hash().Sum64())
}

func (m Metric) String() string {
	return fmt.Sprintf("metric/%x", m.Hash().Sum64())
}

func (s Stream) String() string {
	return fmt.Sprintf("stream/%x", s.Hash().Sum64())
}
