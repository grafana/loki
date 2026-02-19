package physical

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestDataObjScan_TaskCacheID_SameInputsSameID(t *testing.T) {
	tr := TimeRange{Start: time.Unix(0, 0), End: time.Unix(3600, 0)}
	pred := &BinaryExpr{
		Op:    types.BinaryOpGt,
		Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin}},
		Right: NewLiteral(types.Timestamp(0)),
	}
	scan1 := &DataObjScan{
		NodeID:       ulid.Make(),
		Location:     "obj/loc",
		Section:     1,
		StreamIDs:    []int64{10, 20},
		Predicates:   []Expression{pred},
		Projections:  []ColumnExpression{&ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}}},
		MaxTimeRange: tr,
	}
	scan2 := &DataObjScan{
		NodeID:       ulid.Make(), // different NodeID
		Location:     "obj/loc",
		Section:     1,
		StreamIDs:    []int64{10, 20},
		Predicates:   []Expression{pred.Clone()},
		Projections:  []ColumnExpression{&ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}}},
		MaxTimeRange: tr,
	}
	require.Equal(t, scan1.TaskCacheID(), scan2.TaskCacheID(), "same content must produce same task cache ID")
}

func TestDataObjScan_TaskCacheID_DifferentInputsDifferentID(t *testing.T) {
	base := &DataObjScan{
		NodeID:       ulid.Make(),
		Location:     "obj/loc",
		Section:     0,
		StreamIDs:    []int64{1},
		Predicates:   nil,
		Projections:  nil,
		MaxTimeRange: TimeRange{Start: time.Unix(0, 0), End: time.Unix(3600, 0)},
	}
	idBase := base.TaskCacheID()

	t.Run("different location", func(t *testing.T) {
		s := base.Clone().(*DataObjScan)
		s.Location = "other/loc"
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different section", func(t *testing.T) {
		s := base.Clone().(*DataObjScan)
		s.Section = 1
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different stream IDs", func(t *testing.T) {
		s := base.Clone().(*DataObjScan)
		s.StreamIDs = []int64{2}
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different time range", func(t *testing.T) {
		s := base.Clone().(*DataObjScan)
		s.MaxTimeRange = TimeRange{Start: time.Unix(1000, 0), End: time.Unix(2000, 0)}
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different predicates", func(t *testing.T) {
		s := base.Clone().(*DataObjScan)
		s.Predicates = []Expression{NewLiteral("x")}
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different projections", func(t *testing.T) {
		s := base.Clone().(*DataObjScan)
		s.Projections = []ColumnExpression{&ColumnExpr{Ref: types.ColumnRef{Column: "msg", Type: types.ColumnTypeBuiltin}}}
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
}

func TestDataObjScan_TaskCacheID_ClonePreservesID(t *testing.T) {
	scan := &DataObjScan{
		NodeID:       ulid.Make(),
		Location:     "obj/loc",
		Section:     0,
		StreamIDs:    []int64{1, 2, 3},
		Predicates:   []Expression{NewLiteral("a")},
		Projections:  nil,
		MaxTimeRange: TimeRange{Start: time.Unix(0, 0), End: time.Unix(3600, 0)},
	}
	origID := scan.TaskCacheID()
	cloned := scan.Clone().(*DataObjScan)
	require.NotEqual(t, scan.NodeID, cloned.NodeID, "clone must have new NodeID")
	require.Equal(t, origID, cloned.TaskCacheID(), "clone must have same task cache ID")
}

func TestDataObjScan_TaskCacheID_DeterminismReordering(t *testing.T) {
	predA := NewLiteral("a")
	predB := NewLiteral("b")
	projA := &ColumnExpr{Ref: types.ColumnRef{Column: "a", Type: types.ColumnTypeBuiltin}}
	projB := &ColumnExpr{Ref: types.ColumnRef{Column: "b", Type: types.ColumnTypeBuiltin}}

	scan1 := &DataObjScan{
		NodeID:       ulid.Make(),
		Location:     "loc",
		Section:     0,
		StreamIDs:    []int64{3, 1, 2},
		Predicates:   []Expression{predA, predB},
		Projections:  []ColumnExpression{projA, projB},
		MaxTimeRange: TimeRange{Start: time.Unix(0, 0), End: time.Unix(3600, 0)},
	}
	scan2 := &DataObjScan{
		NodeID:       ulid.Make(),
		Location:     "loc",
		Section:     0,
		StreamIDs:    []int64{1, 2, 3}, // different order
		Predicates:   []Expression{predB, predA}, // different order
		Projections:  []ColumnExpression{projB, projA}, // different order
		MaxTimeRange: TimeRange{Start: time.Unix(0, 0), End: time.Unix(3600, 0)},
	}
	require.Equal(t, scan1.TaskCacheID(), scan2.TaskCacheID(), "reordering slices must yield same task cache ID")
}

func TestPointersScan_TaskCacheID_SameInputsSameID(t *testing.T) {
	start := time.Unix(10, 0)
	end := start.Add(time.Hour)
	sel := &ColumnExpr{Ref: types.ColumnRef{Column: "job", Type: types.ColumnTypeLabel}}
	pred := NewLiteral("level=error")

	scan1 := &PointersScan{
		NodeID:     ulid.Make(),
		Location:   "index/0",
		Selector:   sel,
		Predicates: []Expression{pred},
		Start:      start,
		End:        end,
	}
	scan2 := &PointersScan{
		NodeID:     ulid.Make(),
		Location:   "index/0",
		Selector:   sel.Clone(),
		Predicates: []Expression{pred.Clone()},
		Start:      start,
		End:        end,
	}
	require.Equal(t, scan1.TaskCacheID(), scan2.TaskCacheID(), "same content must produce same task cache ID")
}

func TestPointersScan_TaskCacheID_NilSelector(t *testing.T) {
	start := time.Unix(10, 0)
	end := start.Add(time.Hour)
	scan := &PointersScan{
		NodeID:     ulid.Make(),
		Location:   "index/0",
		Selector:   nil,
		Predicates: nil,
		Start:      start,
		End:        end,
	}
	id := scan.TaskCacheID()
	require.NotEmpty(t, id)
	// Same again with nil selector must yield same ID
	scan2 := &PointersScan{
		NodeID:   ulid.Make(),
		Location: "index/0",
		Start:    start,
		End:      end,
	}
	require.Equal(t, id, scan2.TaskCacheID())
}

func TestPointersScan_TaskCacheID_DifferentInputsDifferentID(t *testing.T) {
	start := time.Unix(10, 0)
	end := start.Add(time.Hour)
	base := &PointersScan{
		NodeID:     ulid.Make(),
		Location:   "index/0",
		Selector:   nil,
		Predicates: nil,
		Start:      start,
		End:        end,
	}
	idBase := base.TaskCacheID()

	t.Run("different location", func(t *testing.T) {
		s := base.Clone().(*PointersScan)
		s.Location = "index/1"
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different start", func(t *testing.T) {
		s := base.Clone().(*PointersScan)
		s.Start = start.Add(time.Minute)
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different end", func(t *testing.T) {
		s := base.Clone().(*PointersScan)
		s.End = end.Add(-time.Minute)
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different selector", func(t *testing.T) {
		s := base.Clone().(*PointersScan)
		s.Selector = &ColumnExpr{Ref: types.ColumnRef{Column: "job", Type: types.ColumnTypeLabel}}
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
	t.Run("different predicates", func(t *testing.T) {
		s := base.Clone().(*PointersScan)
		s.Predicates = []Expression{NewLiteral("x")}
		require.NotEqual(t, idBase, s.TaskCacheID())
	})
}

func TestPointersScan_TaskCacheID_ClonePreservesID(t *testing.T) {
	scan := &PointersScan{
		NodeID:     ulid.Make(),
		Location:   "index/0",
		Selector:   &ColumnExpr{Ref: types.ColumnRef{Column: "job", Type: types.ColumnTypeLabel}},
		Predicates: []Expression{NewLiteral("error")},
		Start:      time.Unix(10, 0),
		End:        time.Unix(3700, 0),
	}
	origID := scan.TaskCacheID()
	cloned := scan.Clone().(*PointersScan)
	require.NotEqual(t, scan.NodeID, cloned.NodeID, "clone must have new NodeID")
	require.Equal(t, origID, cloned.TaskCacheID(), "clone must have same task cache ID")
}

func TestPointersScan_TaskCacheID_DeterminismReordering(t *testing.T) {
	predA := NewLiteral("a")
	predB := NewLiteral("b")
	start := time.Unix(10, 0)
	end := start.Add(time.Hour)

	scan1 := &PointersScan{
		NodeID:     ulid.Make(),
		Location:   "index/0",
		Predicates: []Expression{predA, predB},
		Start:      start,
		End:        end,
	}
	scan2 := &PointersScan{
		NodeID:     ulid.Make(),
		Location:   "index/0",
		Predicates: []Expression{predB, predA},
		Start:      start,
		End:        end,
	}
	require.Equal(t, scan1.TaskCacheID(), scan2.TaskCacheID(), "reordering predicates must yield same task cache ID")
}
