// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package base contains shared high-level models that are used between both versions 2 and 3 of OpenAPI.
// These models are consistent across both specifications, except for the Schema.
//
// OpenAPI 3 contains all the same properties that an OpenAPI 2 specification does, and more. The choice
// to not duplicate the schemas is to allow a graceful degradation pattern to be used. Schemas are the most complex
// beats, particularly when polymorphism is used. By re-using the same superset Schema across versions, we can ensure
// that all the latest features are collected, without damaging backwards compatibility.
package base
