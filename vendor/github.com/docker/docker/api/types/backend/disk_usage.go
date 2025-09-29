package backend

import (
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
)

// DiskUsageOptions holds parameters for system disk usage query.
type DiskUsageOptions struct {
	// Containers controls whether container disk usage should be computed.
	Containers bool

	// Images controls whether image disk usage should be computed.
	Images bool

	// Volumes controls whether volume disk usage should be computed.
	Volumes bool
}

// DiskUsage contains the information returned by the backend for the
// GET "/system/df" endpoint.
type DiskUsage struct {
	Images     *image.DiskUsage
	Containers *container.DiskUsage
	Volumes    *volume.DiskUsage
	BuildCache *build.CacheDiskUsage
}
