// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"fmt"
	"strings"
)

// RefType identifies where a reference points to: within the same file (Local),
// to another file on disk (File), or to a remote URL (HTTP).
const (
	Local RefType = iota
	File
	HTTP
)

// RefType is an enum identifying the location type of a reference.
type RefType int

// ExtractedRef represents a parsed reference with its resolved location and type.
type ExtractedRef struct {
	Location string
	Type     RefType
}

// GetFile returns the file path of the reference.
func (r *ExtractedRef) GetFile() string {
	switch r.Type {
	case File, HTTP:
		location := strings.Split(r.Location, "#/")
		return location[0]
	default:
		return r.Location
	}
}

// GetReference returns the reference path of the reference.
func (r *ExtractedRef) GetReference() string {
	switch r.Type {
	case File, HTTP:
		location := strings.Split(r.Location, "#/")
		return fmt.Sprintf("#/%s", location[1])
	default:
		return r.Location
	}
}

// ExtractFileType returns the file extension of the reference.
func ExtractFileType(ref string) FileExtension {
	if strings.HasSuffix(ref, ".yaml") {
		return YAML
	}
	if strings.HasSuffix(ref, ".yml") {
		return YAML
	}
	if strings.HasSuffix(ref, ".json") {
		return JSON
	}
	if strings.HasSuffix(ref, ".js") {
		return JS
	}
	if strings.HasSuffix(ref, ".go") {
		return GO
	}
	if strings.HasSuffix(ref, ".ts") {
		return TS
	}
	if strings.HasSuffix(ref, ".cs") {
		return CS
	}
	if strings.HasSuffix(ref, ".c") {
		return C
	}
	if strings.HasSuffix(ref, ".cpp") {
		return CPP
	}
	if strings.HasSuffix(ref, ".php") {
		return PHP
	}
	if strings.HasSuffix(ref, ".py") {
		return PY
	}
	if strings.HasSuffix(ref, ".html") {
		return HTML
	}
	if strings.HasSuffix(ref, ".md") {
		return MD
	}
	if strings.HasSuffix(ref, ".java") {
		return JAVA
	}
	if strings.HasSuffix(ref, ".rs") {
		return RS
	}
	if strings.HasSuffix(ref, ".zig") {
		return ZIG
	}
	if strings.HasSuffix(ref, ".rb") {
		return RB
	}
	return UNSUPPORTED
}
