package physical

import (
	"slices"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
)

// DataObjLocation is a string that uniquely identifies a data object location in
// object storage.
type DataObjLocation string

// DataObjScan represents a physical plan operation for reading data objects.
// It contains information about the object location, stream IDs, projections,
// predicates for reading data from a data object.
type DataObjScan struct {
	NodeID ulid.ULID

	// Location is the unique name of the data object that is used as source for
	// reading streams.
	Location DataObjLocation
	// Section is the section index inside the data object to scan.
	Section int
	// StreamIDs is a set of stream IDs inside the data object. These IDs are
	// only unique in the context of a single data object.
	StreamIDs []int64
	// Projections are used to limit the columns that are read to the ones
	// provided in the column expressions to reduce the amount of data that needs
	// to be processed.
	Projections []ColumnExpression
	// Predicates are used to filter rows to reduce the amount of rows that are
	// returned. Predicates would almost always contain a time range filter to
	// only read the logs for the requested time range.
	Predicates []Expression
	// The maximum boundary of timestamps that scanning the
	// data object can possibly emit. Does not account for
	// predicates.
	// MaxTimeRange is not read when executing a scan.
	// It can be used as metadata to control physical plan execution.
	MaxTimeRange TimeRange
}

// ID implements the [Node] interface.
// Returns the ULID that uniquely identifies the node in the plan.
func (s *DataObjScan) ID() ulid.ULID { return s.NodeID }

// Clone returns a deep copy of the node with a new unique ID.
func (s *DataObjScan) Clone() Node {
	return &DataObjScan{
		NodeID: ulid.Make(),

		Location:     s.Location,
		Section:      s.Section,
		StreamIDs:    slices.Clone(s.StreamIDs),
		Projections:  cloneExpressions(s.Projections),
		Predicates:   cloneExpressions(s.Predicates),
		MaxTimeRange: s.MaxTimeRange,
	}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*DataObjScan) Type() NodeType {
	return NodeTypeDataObjScan
}

// TaskCacheID returns a deterministic, readable cache key string for this scan task.
// The same Location, Section, StreamIDs, Predicates, Projections, and MaxTimeRange
// produce the same key across plan instances. Callers should hash this for actual cache storage (e.g. cache.HashKey).
func (s *DataObjScan) TaskCacheID() string {
	return cacheKeyStringDataObjScan(s, nil)
}

// TaskCacheIDWithLogger is like TaskCacheID but logs when timestamp predicates
// are clamped to the scan's max_time_range. Use from the executor to observe clamping.
func (s *DataObjScan) TaskCacheIDWithLogger(logger log.Logger) string {
	return cacheKeyStringDataObjScan(s, logger)
}
