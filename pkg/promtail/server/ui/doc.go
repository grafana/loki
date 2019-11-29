// Package ui provides the assets via a virtual filesystem.
package ui

import (
	// The blank import is to make Go modules happy.
	_ "github.com/prometheus/prometheus/pkg/modtimevfs"
	_ "github.com/shurcooL/vfsgen"
)

//go:generate go run -tags=dev assets_generate.go -build_flags="$GOFLAGS"
