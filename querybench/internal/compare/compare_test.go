package compare

import (
	"testing"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/report"
)

func TestKey_IdentityIsContentNotTime(t *testing.T) {
	day1 := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	none := report.SystemStats{}

	base := rangeQuery("sum(x)", day1, 6*time.Hour, 5*time.Minute, 2, nil, 0, none)

	same := rangeQuery("sum(x)", day2, 6*time.Hour, 5*time.Minute, 9, nil, 0, none)
	if key(&base) != key(&same) {
		t.Errorf("same content at a different time must share a key:\n%s\n%s", key(&base), key(&same))
	}

	// Each workload dimension must change the key.
	diff := map[string]report.Query{
		"window": rangeQuery("sum(x)", day1, 3*time.Hour, 5*time.Minute, 2, nil, 0, none),
		"step":   rangeQuery("sum(x)", day1, 6*time.Hour, time.Minute, 2, nil, 0, none),
		"expr":   rangeQuery("sum(y)", day1, 6*time.Hour, 5*time.Minute, 2, nil, 0, none),
		"type":   instantQuery("sum(x)", day1, 6*time.Hour, 2, nil, 0, none),
	}
	for dim, q := range diff {
		if key(&base) == key(&q) {
			t.Errorf("a different %s must change the key, but both were %s", dim, key(&base))
		}
	}
}

func TestAlignQueries_UnionKeepsOrder(t *testing.T) {
	day1 := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	none := report.SystemStats{}

	shared := rangeQuery("sum(shared)", day1, 6*time.Hour, 5*time.Minute, 2, nil, 0, none)
	onlyA := rangeQuery("sum(a)", day1, time.Hour, time.Minute, 2, nil, 0, none)
	sharedLater := rangeQuery("sum(shared)", day2, 6*time.Hour, 5*time.Minute, 4, nil, 0, none)
	onlyB := rangeQuery("sum(b)", day2, 2*time.Hour, 5*time.Minute, 4, nil, 0, none)

	a := &report.Report{Queries: []report.Query{shared, onlyA}}
	b := &report.Report{Queries: []report.Query{sharedLater, onlyB}}

	order, aByKey, bByKey := alignQueries(a, b)

	want := []string{key(&shared), key(&onlyA), key(&onlyB)}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}

	// The shared query resolves in both reports; each unique query in only one.
	if aByKey[key(&shared)] == nil || bByKey[key(&shared)] == nil {
		t.Error("shared query should resolve in both reports")
	}
	if aByKey[key(&onlyA)] == nil || bByKey[key(&onlyA)] != nil {
		t.Error("onlyA should resolve in a only")
	}
	if bByKey[key(&onlyB)] == nil || aByKey[key(&onlyB)] != nil {
		t.Error("onlyB should resolve in b only")
	}
}
