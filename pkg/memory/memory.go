// Package memory provides support for allocating and reusing [Memory] regions.
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

// Memory is a contiguous region of member owned by an [Allocator].
type Memory struct {
	// TODO(rfratto): Do we need the Memory type at all?

	data []byte // Raw data.
}

// Data returns the raw data of the memory region.
func (m *Memory) Data() []byte { return m.data }
