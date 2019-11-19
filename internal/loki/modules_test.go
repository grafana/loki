package loki

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderedDeps(t *testing.T) {
	for _, m := range []moduleName{All, Distributor, Ingester, Querier} {
		deps := orderedDeps(m)
		seen := make(map[moduleName]struct{})
		// make sure that getDeps always orders dependencies correctly.
		for _, d := range deps {
			seen[d] = struct{}{}
			for _, dep := range modules[d].deps {
				if _, ok := seen[dep]; !ok {
					t.Errorf("module %s has dependency %s which has not been seen.", d, dep)
				}
			}
		}
	}
}

func TestOrderedDepsShouldGuaranteeStabilityAcrossMultipleRuns(t *testing.T) {
	initial := orderedDeps(All)

	for i := 0; i < 10; i++ {
		assert.Equal(t, initial, orderedDeps(All))
	}
}

func TestUniqueDeps(t *testing.T) {
	input := []moduleName{Server, Overrides, Distributor, Overrides, Server, Ingester, Server}
	expected := []moduleName{Server, Overrides, Distributor, Ingester}
	assert.Equal(t, expected, uniqueDeps(input))
}
