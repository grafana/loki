package compactionv2

import (
	"context"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

func TestPlan_EmptyInput(t *testing.T) {
	got := Plan(context.Background(), nil, "tenantA", 3)
	require.Nil(t, got, "empty input should produce no tasks")

	got = Plan(context.Background(), []*compactionv2pb.SectionRef{}, "tenantA", 3)
	require.Nil(t, got)
}

func TestPlan_InvalidK_Panics(t *testing.T) {
	sections := []*compactionv2pb.SectionRef{sec("o", 0, "a", "b")}

	require.PanicsWithValue(t, "k must be > 0, got 0", func() {
		Plan(context.Background(), sections, "tenantA", 0)
	}, "k=0 must panic")

	require.PanicsWithValue(t, "k must be > 0, got -1", func() {
		Plan(context.Background(), sections, "tenantA", -1)
	}, "negative k must panic")

	// Empty input + k<=0: panic still fires (k validation is unconditional).
	require.Panics(t, func() {
		Plan(context.Background(), nil, "tenantA", 0)
	}, "k<=0 panic fires even with empty input")
}

func TestPlan_SingleSection(t *testing.T) {
	s := sec("o", 0, "a", "b")
	got := Plan(context.Background(), []*compactionv2pb.SectionRef{s}, "tenantA", 8)

	require.Len(t, got, 1, "P=1 K=8 should produce exactly 1 task")
	require.Equal(t, "tenantA", got[0].Tenant)
	require.Len(t, got[0].Runs, 1)
	require.Equal(t, []*compactionv2pb.SectionRef{s}, got[0].Runs[0].Sections)
}

func TestPlan_KGrouping_P10_K3(t *testing.T) {
	// Construct exactly 10 piles by feeding 10 mutually-overlapping sections.
	// All share MinKey "a" but have different MaxKeys (and stable_ids), so each
	// section creates its own pile.
	sections := make([]*compactionv2pb.SectionRef, 0, 10)
	for i := 0; i < 10; i++ {
		// MaxKey = "z" so all of them overlap pairwise (MinKey "a" < MaxKey "z").
		sections = append(sections, sec("o", int32(i), "a", "z"))
	}

	got := Plan(context.Background(), sections, "tenantA", 3)

	require.Len(t, got, 4, "P=10 K=3 should produce ⌈10/3⌉ = 4 tasks")
	require.Equal(t, "tenantA", got[0].Tenant)

	// Verify task sizes: 3, 3, 3, 1.
	require.Len(t, got[0].Runs, 3)
	require.Len(t, got[1].Runs, 3)
	require.Len(t, got[2].Runs, 3)
	require.Len(t, got[3].Runs, 1)

	// Total piles across all tasks == 10.
	total := 0
	for _, tsk := range got {
		total += len(tsk.Runs)
	}
	require.Equal(t, 10, total)
}

func TestPlan_KGreaterThanP(t *testing.T) {
	// 3 mutually-overlapping sections (P=3); K=100 → 1 task with 3 piles.
	sections := []*compactionv2pb.SectionRef{
		sec("o", 0, "a", "z"),
		sec("o", 1, "a", "z"),
		sec("o", 2, "a", "z"),
	}

	got := Plan(context.Background(), sections, "tenantB", 100)
	require.Len(t, got, 1)
	require.Equal(t, "tenantB", got[0].Tenant)
	require.Len(t, got[0].Runs, 3)
}

func TestPlan_Determinism(t *testing.T) {
	// Mix of overlapping and non-overlapping sections.
	base := []*compactionv2pb.SectionRef{
		sec("o", 0, "01", "03"),
		sec("o", 1, "02", "04"),
		sec("o", 2, "05", "07"),
		sec("o", 3, "06", "10"),
		sec("o", 4, "11", "13"),
		sec("o", 5, "12", "14"),
		sec("o", 6, "15", "18"),
		sec("p", 0, "02", "06"),
		sec("p", 1, "08", "09"),
	}

	want := Plan(context.Background(), append([]*compactionv2pb.SectionRef(nil), base...), "tenantA", 3)

	r := rand.New(rand.NewSource(1337))
	for trial := 0; trial < 10; trial++ {
		shuffled := append([]*compactionv2pb.SectionRef(nil), base...)
		r.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got := Plan(context.Background(), shuffled, "tenantA", 3)

		require.Equal(t, len(want), len(got), "trial %d: task count must be deterministic", trial)
		for i := range want {
			require.Equal(t, want[i].Tenant, got[i].Tenant)
			require.Equal(t, len(want[i].Runs), len(got[i].Runs), "trial %d: task %d run count", trial, i)
			for j := range want[i].Runs {
				require.Equal(t, want[i].Runs[j].Sections, got[i].Runs[j].Sections,
					"trial %d: task %d run %d sections", trial, i, j)
			}
		}
	}
}

func TestPlan_TenantPassThrough(t *testing.T) {
	sections := []*compactionv2pb.SectionRef{sec("o", 0, "a", "b"), sec("o", 1, "a", "c")}
	for _, tenant := range []string{"", "tenant-1", "tenant_with_strange_chars-A.B/C"} {
		got := Plan(context.Background(), sections, tenant, 1)
		for _, tsk := range got {
			require.Equal(t, tenant, tsk.Tenant)
		}
	}
}
