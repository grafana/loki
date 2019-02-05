package loki

import (
	"testing"
)

func TestGetDeps(t *testing.T) {
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
