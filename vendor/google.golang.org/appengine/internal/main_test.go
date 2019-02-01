// +build !appengine

package internal

import (
	"go/build"
	"path/filepath"
	"testing"
)

func TestFindMainPath(t *testing.T) {
	// Tests won't have package main, instead they have testing.tRunner
	want := filepath.Join(build.Default.GOROOT, "src", "testing", "testing.go")
	got := findMainPath()
	if want != got {
		t.Errorf("findMainPath: want %s, got %s", want, got)
	}
}
