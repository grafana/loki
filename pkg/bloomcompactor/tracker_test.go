package bloomcompactor

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func mkTblRange(tenant string, tbl config.DayTime, from, through model.Fingerprint) *tenantTableRange {
	return &tenantTableRange{
		tenant:         tenant,
		table:          config.NewDayTable(tbl, ""),
		ownershipRange: v1.NewBounds(from, through),
	}
}

func updateTracker(tr *compactionTracker, tt *tenantTableRange, lastFP model.Fingerprint) {
	tr.update(tt.tenant, tt.table.DayTime, tt.ownershipRange, lastFP)
}

func TestCompactionTrackerClipsRange(t *testing.T) {
	// test invalid table number
	tracker, err := newCompactionTracker(1)
	require.NoError(t, err)

	day1 := parseDayTime("2024-01-01")
	tracker.registerTable(day1, 1)

	work := mkTblRange("a", day1, 0, 10)
	updateTracker(tracker, work, 0)
	require.Equal(t, 0., tracker.progress())
	updateTracker(tracker, work, work.ownershipRange.Min)
	require.Equal(t, 0., tracker.progress())
	updateTracker(tracker, work, 5)
	require.Equal(t, 0.5, tracker.progress())
	updateTracker(tracker, work, work.ownershipRange.Max*2)
	require.Equal(t, 1., tracker.progress())
	updateTracker(tracker, work, work.ownershipRange.Max)
	require.Equal(t, 1., tracker.progress())
}

func TestCompactionTrackerFull(t *testing.T) {
	// test invalid table number
	_, err := newCompactionTracker(0)
	require.Error(t, err)

	tracker, err := newCompactionTracker(2)
	require.NoError(t, err)

	day1 := parseDayTime("2024-01-01")
	day2 := parseDayTime("2024-01-02")

	tracker.registerTable(day1, 2)
	tracker.registerTable(day2, 3)
	require.Equal(t, 0., tracker.progress())

	aDayOneOffsetZero := mkTblRange("a", day1, 0, 10)
	aDayOneOffsetOne := mkTblRange("a", day1, 40, 50)
	bDayOneOffsetZero := mkTblRange("b", day1, 10, 20)

	// register the  workloads for day0_tenantA
	updateTracker(tracker, aDayOneOffsetZero, 0)
	updateTracker(tracker, aDayOneOffsetOne, 0)

	require.Equal(t, 0., tracker.progress())
	updateTracker(tracker, aDayOneOffsetZero, aDayOneOffsetZero.ownershipRange.Max) // simulate finish
	require.Equal(t, 0.125, tracker.progress())
	updateTracker(tracker, aDayOneOffsetOne, aDayOneOffsetOne.ownershipRange.Max) // simulate finish
	require.Equal(t, 0.25, tracker.progress())

	// register the  workloads for day0_tenantB
	updateTracker(tracker, bDayOneOffsetZero, 0)

	require.Equal(t, 0.25, tracker.progress())
	// simulate half finish (partial workload progress)
	updateTracker(
		tracker,
		bDayOneOffsetZero,
		bDayOneOffsetZero.ownershipRange.Min+model.Fingerprint(bDayOneOffsetZero.ownershipRange.Range())/2,
	)
	require.Equal(t, 0.375, tracker.progress())
	// simulate finish
	updateTracker(tracker, bDayOneOffsetZero, bDayOneOffsetZero.ownershipRange.Max)
	require.Equal(t, 0.5, tracker.progress())

	aDayTwoOffsetZero := mkTblRange("a", day2, 0, 10)
	bDayTwoOffsetZero := mkTblRange("b", day2, 10, 20)
	cDayTwoOffsetZero := mkTblRange("c", day2, 20, 30)
	updateTracker(tracker, aDayTwoOffsetZero, 0)
	updateTracker(tracker, bDayTwoOffsetZero, 0)
	updateTracker(tracker, cDayTwoOffsetZero, 0)
	require.Equal(t, 0.5, tracker.progress())

	// simulate finish for the a & b
	updateTracker(tracker, aDayTwoOffsetZero, aDayTwoOffsetZero.ownershipRange.Max)
	updateTracker(tracker, bDayTwoOffsetZero, bDayTwoOffsetZero.ownershipRange.Max)
	require.Equal(t, 0.833, tracker.progress())

	// simulate finish for the c
	updateTracker(tracker, cDayTwoOffsetZero, cDayTwoOffsetZero.ownershipRange.Max)
	require.Equal(t, 1., tracker.progress())
}
