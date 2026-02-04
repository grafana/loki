package compute_test

import (
	"github.com/grafana/loki/v3/pkg/memory"
)

func selectionMask(alloc *memory.Allocator, values ...bool) memory.Bitmap {
	bmap := memory.NewBitmap(alloc, len(values))
	for _, value := range values {
		bmap.Append(value)
	}
	return bmap
}
