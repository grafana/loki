// Copyright 2021 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !windows
// +build !windows

package renameio

import "os"

// Option is the interface implemented by all configuration function return
// values.
type Option interface {
	apply(*config)
}

type optionFunc func(*config)

func (fn optionFunc) apply(cfg *config) {
	fn(cfg)
}

// WithTempDir configures the directory to use for temporary, uncommitted
// files. Suitable for using a cached directory from
// TempDir(filepath.Base(path)).
func WithTempDir(dir string) Option {
	return optionFunc(func(cfg *config) {
		cfg.dir = dir
	})
}

// WithPermissions sets the permissions for the target file while respecting
// the umask(2). Bits set in the umask are removed from the permissions given
// unless IgnoreUmask is used.
func WithPermissions(perm os.FileMode) Option {
	perm &= os.ModePerm
	return optionFunc(func(cfg *config) {
		cfg.createPerm = perm
	})
}

// IgnoreUmask causes the permissions configured using WithPermissions to be
// applied directly without applying the umask.
func IgnoreUmask() Option {
	return optionFunc(func(cfg *config) {
		cfg.ignoreUmask = true
	})
}

// WithStaticPermissions sets the permissions for the target file ignoring the
// umask(2). This is equivalent to calling Chmod() on the file handle or using
// WithPermissions in combination with IgnoreUmask.
func WithStaticPermissions(perm os.FileMode) Option {
	perm &= os.ModePerm
	return optionFunc(func(cfg *config) {
		cfg.chmod = &perm
	})
}

// WithExistingPermissions configures the file creation to try to use the
// permissions from an already existing target file. If the target file doesn't
// exist yet or is not a regular file the default permissions are used unless
// overridden using WithPermissions or WithStaticPermissions.
func WithExistingPermissions() Option {
	return optionFunc(func(c *config) {
		c.attemptPermCopy = true
	})
}
