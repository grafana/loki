package util

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiskUsage(t *testing.T) {
	fsObjectsDir := t.TempDir()
	diskUsage, _ := DiskUsage(fsObjectsDir)

	require.Equal(t, "float64", fmt.Sprintf("%T", diskUsage.UsedPercent), "diskUsage.UsedPercent is not float64")
	require.LessOrEqual(t, diskUsage.UsedPercent, float64(100), "diskUsage.UsedPercent is miscalculated")
}
