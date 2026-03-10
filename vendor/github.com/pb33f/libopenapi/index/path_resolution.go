// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi/utils"
)

// resolveRelativeFilePath resolves a relative file reference against a base directory.
// It prefers paths that actually exist in the configured local file systems, falling
// back to defRoot if no match is found.
func (index *SpecIndex) resolveRelativeFilePath(defRoot, ref string) string {
	sep := string(os.PathSeparator)
	resolveAbs := func(base string) string {
		p := utils.CheckPathOverlap(base, ref, sep)
		abs, _ := filepath.Abs(p)
		return abs
	}

	fallback := resolveAbs(defRoot)

	if index == nil || index.config == nil || index.config.BaseURL != nil || index.rolodex == nil {
		return fallback
	}

	// Prefer the path relative to the current file if it exists.
	if len(index.rolodex.localFS) > 0 {
		bases := make([]string, 0, len(index.rolodex.localFS))
		for base := range index.rolodex.localFS {
			bases = append(bases, base)
		}
		sort.Strings(bases)
		for _, base := range bases {
			if pathExistsInFS(base, index.rolodex.localFS[base], fallback) {
				return fallback
			}
		}
	}

	// Prefer the configured BasePath if present and it yields an existing file.
	if index.config.BasePath != "" {
		baseAbs, _ := filepath.Abs(index.config.BasePath)
		if fsys, ok := index.rolodex.localFS[baseAbs]; ok {
			cand := resolveAbs(baseAbs)
			if pathExistsInFS(baseAbs, fsys, cand) {
				return cand
			}
		}
	}

	// Otherwise, try each registered local filesystem base directory.
	if len(index.rolodex.localFS) > 0 {
		bases := make([]string, 0, len(index.rolodex.localFS))
		for base := range index.rolodex.localFS {
			bases = append(bases, base)
		}
		sort.Strings(bases)
		for _, base := range bases {
			cand := resolveAbs(base)
			if pathExistsInFS(base, index.rolodex.localFS[base], cand) {
				return cand
			}
		}
	}

	return fallback
}

// ResolveRelativeFilePath is a public wrapper for resolving local file references.
func (index *SpecIndex) ResolveRelativeFilePath(defRoot, ref string) string {
	return index.resolveRelativeFilePath(defRoot, ref)
}

func pathExistsInFS(baseDir string, fsys fs.FS, absPath string) bool {
	if !filepath.IsAbs(absPath) {
		absPath, _ = filepath.Abs(utils.CheckPathOverlap(baseDir, absPath, string(os.PathSeparator)))
	}

	if lfs, ok := fsys.(*LocalFS); ok {
		if lfs.fsConfig != nil && lfs.fsConfig.DirFS != nil {
			rel, err := filepath.Rel(baseDir, absPath)
			if err != nil {
				return false
			}
			rel = filepath.ToSlash(rel)
			if !fs.ValidPath(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return false
			}
			_, err = fs.Stat(lfs.fsConfig.DirFS, rel)
			return err == nil
		}

		rel, err := filepath.Rel(baseDir, absPath)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return false
		}
		_, err = os.Stat(absPath)
		return err == nil
	}

	rel, err := filepath.Rel(baseDir, absPath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if !fs.ValidPath(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	_, err = fs.Stat(fsys, rel)
	return err == nil
}
