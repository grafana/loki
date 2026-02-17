package mmdberrors

import (
	"fmt"
	"strconv"
	"strings"
)

// ContextualError provides detailed error context with offset and path information.
// This is only allocated when an error actually occurs, ensuring zero allocation
// on the happy path.
type ContextualError struct {
	Err    error
	Path   string
	Offset uint
}

func (e ContextualError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("at offset %d, path %s: %v", e.Offset, e.Path, e.Err)
	}
	return fmt.Sprintf("at offset %d: %v", e.Offset, e.Err)
}

func (e ContextualError) Unwrap() error {
	return e.Err
}

// ErrorContextTracker is an optional interface that can be used to track
// path context for better error messages. Only used when explicitly enabled
// and only allocates when an error occurs.
type ErrorContextTracker interface {
	// BuildPath constructs a path string for the current decoder state.
	// This is only called when an error occurs, so allocation is acceptable.
	BuildPath() string
}

// WrapWithContext wraps an error with offset and optional path context.
// This function is designed to have zero allocation on the happy path -
// it only allocates when an error actually occurs.
func WrapWithContext(err error, offset uint, tracker ErrorContextTracker) error {
	if err == nil {
		return nil // Zero allocation - no error to wrap
	}

	// Only allocate when we actually have an error
	ctxErr := ContextualError{
		Offset: offset,
		Err:    err,
	}

	// Only build path if tracker is provided (opt-in behavior)
	if tracker != nil {
		ctxErr.Path = tracker.BuildPath()
	}

	return ctxErr
}

// PathBuilder helps build JSON-pointer-like paths efficiently.
// Only used when an error occurs, so allocations are acceptable here.
type PathBuilder struct {
	segments []string
}

// NewPathBuilder creates a new path builder.
func NewPathBuilder() *PathBuilder {
	return &PathBuilder{
		segments: make([]string, 0, 8), // Pre-allocate for common depth
	}
}

// BuildPath implements ErrorContextTracker interface.
func (p *PathBuilder) BuildPath() string {
	return p.Build()
}

// PushMap adds a map key to the path.
func (p *PathBuilder) PushMap(key string) {
	p.segments = append(p.segments, key)
}

// PushSlice adds a slice index to the path.
func (p *PathBuilder) PushSlice(index int) {
	p.segments = append(p.segments, strconv.Itoa(index))
}

// PrependMap adds a map key to the beginning of the path (for retroactive building).
func (p *PathBuilder) PrependMap(key string) {
	p.segments = append([]string{key}, p.segments...)
}

// PrependSlice adds a slice index to the beginning of the path (for retroactive building).
func (p *PathBuilder) PrependSlice(index int) {
	p.segments = append([]string{strconv.Itoa(index)}, p.segments...)
}

// Pop removes the last segment from the path.
func (p *PathBuilder) Pop() {
	if len(p.segments) > 0 {
		p.segments = p.segments[:len(p.segments)-1]
	}
}

// Build constructs the full path string.
func (p *PathBuilder) Build() string {
	if len(p.segments) == 0 {
		return "/"
	}
	return "/" + strings.Join(p.segments, "/")
}

// Reset clears all segments for reuse.
func (p *PathBuilder) Reset() {
	p.segments = p.segments[:0]
}

// ParseAndExtend parses an existing path and extends this builder with those segments.
// This is used for retroactive path building during error unwinding.
func (p *PathBuilder) ParseAndExtend(path string) {
	if path == "" || path == "/" {
		return
	}

	// Remove leading slash and split
	if path[0] == '/' {
		path = path[1:]
	}

	segments := strings.SplitSeq(path, "/")
	for segment := range segments {
		if segment != "" {
			p.segments = append(p.segments, segment)
		}
	}
}
