// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package swag

import "github.com/go-openapi/swag/fileutils"

// GOPATHKey represents the env key for gopath
//
// Deprecated: use [fileutils.GOPATHKey] instead.
const GOPATHKey = fileutils.GOPATHKey

// File represents an uploaded file.
//
// Deprecated: use [fileutils.File] instead.
type File = fileutils.File

// FindInSearchPath finds a package in a provided lists of paths.
//
// Deprecated: use [fileutils.FindInSearchPath] instead.
func FindInSearchPath(searchPath, pkg string) string {
	return fileutils.FindInSearchPath(searchPath, pkg)
}

// FindInGoSearchPath finds a package in the $GOPATH:$GOROOT
//
// Deprecated: use [fileutils.FindInGoSearchPath] instead.
func FindInGoSearchPath(pkg string) string { return fileutils.FindInGoSearchPath(pkg) }

// FullGoSearchPath gets the search paths for finding packages
//
// Deprecated: use [fileutils.FullGoSearchPath] instead.
func FullGoSearchPath() string { return fileutils.FullGoSearchPath() }
