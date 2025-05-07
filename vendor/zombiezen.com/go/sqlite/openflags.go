// Copyright 2021 Roxy Light
// SPDX-License-Identifier: ISC

package sqlite

import (
	"fmt"
	"strings"

	lib "modernc.org/sqlite/lib"
)

// OpenFlags are [flags] used when opening a [Conn] via [OpenConn].
//
// [flags]: https://www.sqlite.org/c3ref/c_open_autoproxy.html
type OpenFlags uint

// One of the following flags must be passed to [OpenConn].
const (
	// OpenReadOnly opens the database in read-only mode.
	// If the database does not already exist, an error is returned.
	OpenReadOnly OpenFlags = lib.SQLITE_OPEN_READONLY
	// OpenReadWrite opens the database for reading and writing if possible,
	// or reading only if the file is write protected by the operating system.
	// If the database does not already exist,
	// an error is returned unless OpenCreate is also passed.
	OpenReadWrite OpenFlags = lib.SQLITE_OPEN_READWRITE
)

// Optional flags to pass to [OpenConn].
const (
	// OpenCreate will create the file if it does not already exist.
	// It is only valid with [OpenReadWrite].
	OpenCreate OpenFlags = lib.SQLITE_OPEN_CREATE
	// OpenURI allows the path to be interpreted as a URI.
	OpenURI OpenFlags = lib.SQLITE_OPEN_URI
	// OpenMemory will be opened as an in-memory database.
	// The path is ignored unless [OpenSharedCache] is used.
	OpenMemory OpenFlags = lib.SQLITE_OPEN_MEMORY
	// OpenSharedCache opens the database with [shared-cache].
	// This is mostly only useful for sharing in-memory databases:
	// it's [not recommended] for other purposes.
	//
	// [shared-cache]: https://www.sqlite.org/sharedcache.html
	// [not recommended]: https://www.sqlite.org/sharedcache.html#dontuse
	OpenSharedCache OpenFlags = lib.SQLITE_OPEN_SHAREDCACHE
	// OpenPrivateCache forces the database to not use shared-cache.
	OpenPrivateCache OpenFlags = lib.SQLITE_OPEN_PRIVATECACHE
	// OpenWAL enables the [write-ahead log] for the database.
	//
	// [write-ahead log]: https://www.sqlite.org/wal.html
	OpenWAL OpenFlags = lib.SQLITE_OPEN_WAL

	// OpenNoMutex has no effect.
	//
	// Deprecated: This flag is now implied.
	OpenNoMutex OpenFlags = lib.SQLITE_OPEN_NOMUTEX
	// OpenFullMutex has no effect.
	//
	// Deprecated: This flag has no equivalent and is ignored.
	OpenFullMutex OpenFlags = lib.SQLITE_OPEN_FULLMUTEX
)

// String returns a pipe-separated list of the C constant names set in flags.
func (flags OpenFlags) String() string {
	var parts []string
	if flags&OpenReadOnly != 0 {
		parts = append(parts, "SQLITE_OPEN_READONLY")
		flags &^= OpenReadOnly
	}
	if flags&OpenReadWrite != 0 {
		parts = append(parts, "SQLITE_OPEN_READWRITE")
		flags &^= OpenReadWrite
	}
	if flags&OpenCreate != 0 {
		parts = append(parts, "SQLITE_OPEN_CREATE")
		flags &^= OpenCreate
	}
	if flags&OpenURI != 0 {
		parts = append(parts, "SQLITE_OPEN_URI")
		flags &^= OpenURI
	}
	if flags&OpenMemory != 0 {
		parts = append(parts, "SQLITE_OPEN_MEMORY")
		flags &^= OpenMemory
	}
	if flags&OpenNoMutex != 0 {
		parts = append(parts, "SQLITE_OPEN_NOMUTEX")
		flags &^= OpenNoMutex
	}
	if flags&OpenFullMutex != 0 {
		parts = append(parts, "SQLITE_OPEN_FULLMUTEX")
		flags &^= OpenFullMutex
	}
	if flags&OpenSharedCache != 0 {
		parts = append(parts, "SQLITE_OPEN_SHAREDCACHE")
		flags &^= OpenSharedCache
	}
	if flags&OpenPrivateCache != 0 {
		parts = append(parts, "SQLITE_OPEN_PRIVATECACHE")
		flags &^= OpenPrivateCache
	}
	if flags&OpenWAL != 0 {
		parts = append(parts, "SQLITE_OPEN_WAL")
		flags &^= OpenWAL
	}
	if flags != 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%#x", uint(flags)))
	}
	return strings.Join(parts, "|")
}
