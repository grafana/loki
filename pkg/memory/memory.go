// Package memory provides support for allocating and reusing contiguous
// [Region]s of memory.
//
// An [Allocator] supports reclaiming memory regions for reuse, invaliding
// existing regions. Using a memory region after it has been reclaimed produces
// undefined behavior, so caution must be taken to ensure that the lifetime of
// Memory does not exceed the lifetime of the owning Allocator.
//
// Utility packages are provided to make it easier to work with memory regions:
//
//   - [github.com/grafana/loki/v3/pkg/memory/buffer] for resizable typed buffers.
//   - [github.com/grafana/loki/v3/pkg/memory/bitmap] for bitmaps.
//
// Memory is EXPERIMENTAL and is currently only intended for use by
// [github.com/grafana/loki/v3/pkg/dataobj].
package memory

// Region is a contiguous region of memory owned by an [Allocator].
type Region struct {
	// TODO(rfratto): Do we need the Memory type at all?

	data []byte // Raw data.
}

// Data returns the raw data of the memory region.
func (m *Region) Data() []byte { return m.data }
