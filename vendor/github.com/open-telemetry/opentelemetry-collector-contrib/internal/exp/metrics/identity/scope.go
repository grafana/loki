// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package identity // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"

import (
	"fmt"
	"hash"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatautil"
)

type scope = Scope

type Scope struct {
	resource resource

	name    string
	version string
	attrs   [16]byte
}

func (s Scope) Hash() hash.Hash64 {
	sum := s.resource.Hash()
	sum.Write([]byte(s.name))
	sum.Write([]byte(s.version))
	sum.Write(s.attrs[:])
	return sum
}

func (s Scope) Resource() Resource {
	return s.resource
}

func (s Scope) String() string {
	return fmt.Sprintf("scope/%x", s.Hash().Sum64())
}

func OfScope(res Resource, scope pcommon.InstrumentationScope) Scope {
	return Scope{
		resource: res,
		name:     scope.Name(),
		version:  scope.Version(),
		attrs:    pdatautil.MapHash(scope.Attributes()),
	}
}
