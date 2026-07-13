package compactionv2

import (
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

type fakeRun struct {
	sections []compactionv2pb.SectionRef
	size     uint64
}

func (f fakeRun) Sections() []compactionv2pb.SectionRef { return f.sections }
func (f fakeRun) Size() uint64                          { return f.size }

func TestPlan_EmptyInput(t *testing.T) {
	require.Nil(t, Plan(nil, "tenantA", 3, nil))
	require.Nil(t, Plan([]Run{}, "tenantA", 3, nil))
}

func TestPlan_InvalidK_Panics(t *testing.T) {
	runs := []Run{fakeRun{sections: []compactionv2pb.SectionRef{{ObjectPath: "a"}}}}
	require.PanicsWithValue(t, "k must be > 0, got 0", func() { Plan(runs, "tenantA", 0, nil) })
	require.PanicsWithValue(t, "k must be > 0, got -1", func() { Plan(runs, "tenantA", -1, nil) })
	// nil runs (typed []Run) with k<=0 still panics on k before the empty check.
	require.PanicsWithValue(t, "k must be > 0, got 0", func() { Plan(nil, "tenantA", 0, nil) })
}

func TestPlan_SingleRun(t *testing.T) {
	s := compactionv2pb.SectionRef{ObjectPath: "a", SectionIndex: 0}
	got := Plan([]Run{fakeRun{sections: []compactionv2pb.SectionRef{s}}}, "tenantA", 8, nil)
	require.Len(t, got, 1)
	require.Equal(t, "tenantA", got[0].Tenant)
	require.Len(t, got[0].Runs, 1)
}

func TestPlan_KGrouping_P10_K3(t *testing.T) {
	runs := make([]Run, 10)
	for i := range runs {
		runs[i] = fakeRun{sections: []compactionv2pb.SectionRef{{ObjectPath: "o", SectionIndex: int64(i)}}}
	}
	got := Plan(runs, "tenantA", 3, nil)
	require.Len(t, got, 4) // ceil(10/3)
	require.Len(t, got[0].Runs, 3)
	require.Len(t, got[1].Runs, 3)
	require.Len(t, got[2].Runs, 3)
	require.Len(t, got[3].Runs, 1)
}

func TestPlan_KGreaterThanP(t *testing.T) {
	runs := make([]Run, 3)
	for i := range runs {
		runs[i] = fakeRun{sections: []compactionv2pb.SectionRef{{ObjectPath: "o", SectionIndex: int64(i)}}}
	}
	got := Plan(runs, "tenantB", 100, nil)
	require.Len(t, got, 1)
	require.Len(t, got[0].Runs, 3)
}

func TestPlan_TenantPassThrough(t *testing.T) {
	runs := []Run{fakeRun{sections: []compactionv2pb.SectionRef{{ObjectPath: "a"}}}}
	for _, tenant := range []string{"t1", "t2"} {
		got := Plan(runs, tenant, 1, nil)
		require.Equal(t, tenant, got[0].Tenant)
	}
}

func TestPlan_StampsSortSchema(t *testing.T) {
	schema := []string{"label:service_name"}
	runs := make([]Run, 3)
	for i := range runs {
		runs[i] = fakeRun{sections: []compactionv2pb.SectionRef{{ObjectPath: "o", SectionIndex: int64(i)}}}
	}
	tasks := Plan(runs, "t1", 2, schema)
	require.NotEmpty(t, tasks)
	for _, ts := range tasks {
		require.Equal(t, schema, ts.SortSchema)
	}
}

func TestPlan_NilSortSchemaStaysEmpty(t *testing.T) {
	runs := []Run{fakeRun{sections: []compactionv2pb.SectionRef{{ObjectPath: "a"}}}}
	tasks := Plan(runs, "t1", 2, nil)
	require.NotEmpty(t, tasks)
	require.Empty(t, tasks[0].SortSchema)
}

func TestRun_SizeAndSections(t *testing.T) {
	s1 := compactionv2pb.SectionRef{ObjectPath: "a", SectionIndex: 0, UncompressedSize: 100}
	s2 := compactionv2pb.SectionRef{ObjectPath: "a", SectionIndex: 1, UncompressedSize: 250}

	var r Run = &run{sections: []compactionv2pb.SectionRef{s1, s2}}

	require.Equal(t, uint64(350), r.Size())
	require.Equal(t, []compactionv2pb.SectionRef{s1, s2}, r.Sections())
}

func TestRun_SizeEmpty(t *testing.T) {
	var r Run = &run{sections: nil}
	require.Equal(t, uint64(0), r.Size())
	require.Empty(t, r.Sections())
}

func TestCalculateRuns_EmptyInput(t *testing.T) {
	require.Empty(t, CalculateRuns(nil))
	require.Empty(t, CalculateRuns([]compactionv2pb.SectionRef{}))
}

func TestCalculateRuns_WrapsRunsAndSortsInPlace(t *testing.T) {
	// Two non-overlapping same-tuple sections (disjoint, ordered times) chain
	// into one run; passed out of order to prove in-place sorting.
	a := compactionv2pb.SectionRef{ObjectPath: "a", SectionIndex: 0, MinKey: []string{"svc"}, MaxKey: []string{"svc"}, MinTimestamp: 10, MaxTimestamp: 20, UncompressedSize: 5}
	b := compactionv2pb.SectionRef{ObjectPath: "b", SectionIndex: 0, MinKey: []string{"svc"}, MaxKey: []string{"svc"}, MinTimestamp: 30, MaxTimestamp: 40, UncompressedSize: 7}

	input := []compactionv2pb.SectionRef{b, a}
	runs := CalculateRuns(input)

	require.Len(t, runs, 1)
	require.Equal(t, uint64(12), runs[0].Size())
	require.Equal(t, []compactionv2pb.SectionRef{a, b}, input, "input sorted in place")
}

func TestIsTerminal(t *testing.T) {
	const floor = uint64(100)
	run := func(size uint64) Run { return fakeRun{size: size} }

	tests := []struct {
		name     string
		runs     []Run
		terminal bool
	}{
		{"no runs", nil, true},
		{"single run below floor", []Run{run(1)}, true},
		{"single run above floor", []Run{run(500)}, true},
		{"two runs total >= floor", []Run{run(60), run(60)}, false},
		{"two runs total exactly floor", []Run{run(50), run(50)}, false},
		{"two runs total below floor", []Run{run(10), run(20)}, true},
		{"many tiny runs below floor (small tenant)", []Run{run(5), run(5), run(5)}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.terminal, IsTerminal(tc.runs, floor))
		})
	}
}
