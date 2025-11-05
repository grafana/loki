// Copyright 2023 Roxy Light
// SPDX-License-Identifier: ISC

package sqlite

import (
	"fmt"

	"modernc.org/libc"
	lib "modernc.org/sqlite/lib"
)

// A Backup represents a copy operation between two database connections.
// See https://www.sqlite.org/backup.html for more details.
type Backup struct {
	tls *libc.TLS
	ptr uintptr
}

// NewBackup creates a [Backup] that copies from one connection to another.
// The database name is "main" or "" for the main database,
// "temp" for the temporary database,
// or the name specified after the AS keyword in an ATTACH statement for an attached database.
// If src and dst are the same connection, NewBackup will return an error.
// It is the caller's responsibility to call [Backup.Close] on the returned Backup object.
func NewBackup(dst *Conn, dstName string, src *Conn, srcName string) (*Backup, error) {
	cDstName, freeCDstName, err := cDBName(dstName)
	if err != nil {
		return nil, fmt.Errorf("sqlite: new backup: %w", err)
	}
	defer freeCDstName()
	cSrcName, freeCSrcName, err := cDBName(srcName)
	if err != nil {
		return nil, fmt.Errorf("sqlite: new backup: %w", err)
	}
	defer freeCSrcName()
	backupPtr := lib.Xsqlite3_backup_init(dst.tls, dst.conn, cDstName, src.conn, cSrcName)
	if backupPtr == 0 {
		res := ResultCode(lib.Xsqlite3_errcode(dst.tls, dst.conn))
		return nil, fmt.Errorf("sqlite: new backup: %w", dst.extreserr(res))
	}
	return &Backup{dst.tls, backupPtr}, nil
}

// Step copies up to n pages from the source database to the destination database.
// If n is negative, all remaining source pages are copied.
// more will be true if there are pages still remaining to be copied.
// Step may return both an error and that more pages are still remaining:
// this indicates the error is temporary and that Step can be retried.
func (b *Backup) Step(n int) (more bool, err error) {
	res := ResultCode(lib.Xsqlite3_backup_step(b.tls, b.ptr, int32(n)))
	switch res {
	case ResultOK:
		return true, nil
	case ResultDone:
		return false, nil
	case ResultBusy, ResultLocked:
		// SQLITE_BUSY and SQLITE_LOCKED are retriable errors.
		return true, fmt.Errorf("sqlite: backup step: %w", res.ToError())
	default:
		return false, fmt.Errorf("sqlite: backup step: %w", res.ToError())
	}
}

// Remaining returns the number of pages still to be backed up
// at the conclusion of the most recent call to [Backup.Step].
// The return value of Remaining before calling [Backup.Step] is undefined.
// If the source database is modified in a way that changes the number of pages remaining,
// that change is not reflected in the output until after the next call to [Backup.Step].
func (b *Backup) Remaining() int {
	return int(lib.Xsqlite3_backup_remaining(b.tls, b.ptr))
}

// PageCount returns the total number of pages in the source database
// at the conclusion of the most recent call to [Backup.Step].
// The return value of PageCount before calling [Backup.Step] is undefined.
// If the source database is modified in a way that changes the size of the source database,
// that change is not reflected in the output until after the next call to [Backup.Step].
func (b *Backup) PageCount() int {
	return int(lib.Xsqlite3_backup_pagecount(b.tls, b.ptr))
}

// Close releases all resources associated with the backup.
// If [Backup.Step] has not yet returned (false, nil),
// then any active write transaction on the destination database is rolled back.
func (b *Backup) Close() error {
	// The error from sqlite3_backup_finish indicates whether
	// a previous call to sqlite3_backup_step returned an error.
	// Since we're assuming that the caller will handle errors as they arise,
	// we always return nil from Close.
	// However, I'm not fully confident that Close will always be infallible,
	// so I'm not documenting this as part of the API.
	lib.Xsqlite3_backup_finish(b.tls, b.ptr)
	return nil
}
