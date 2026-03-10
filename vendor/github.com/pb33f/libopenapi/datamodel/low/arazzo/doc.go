// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package arazzo contains low-level Arazzo models.
//
// Arazzo low models include an *index.SpecIndex in Build signatures to remain
// compatible with the shared low.Buildable interface and generic extraction
// pipeline used across low-level model packages.
//
// In current Arazzo parsing paths, no SpecIndex is built and nil is passed for
// idx (for example via libopenapi.NewArazzoDocument), so GetIndex() will
// typically return nil unless callers explicitly provide an index.
package arazzo
