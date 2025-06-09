// Copyright 2021 Roxy Light
// SPDX-License-Identifier: ISC

package sqlite

import (
	"errors"
	"fmt"

	"modernc.org/libc"
	lib "modernc.org/sqlite/lib"
)

// ResultCode is an SQLite extended result code.
type ResultCode int32

// Primary result codes.
const (
	ResultOK         ResultCode = lib.SQLITE_OK
	ResultError      ResultCode = lib.SQLITE_ERROR
	ResultInternal   ResultCode = lib.SQLITE_INTERNAL
	ResultPerm       ResultCode = lib.SQLITE_PERM
	ResultAbort      ResultCode = lib.SQLITE_ABORT
	ResultBusy       ResultCode = lib.SQLITE_BUSY
	ResultLocked     ResultCode = lib.SQLITE_LOCKED
	ResultNoMem      ResultCode = lib.SQLITE_NOMEM
	ResultReadOnly   ResultCode = lib.SQLITE_READONLY
	ResultInterrupt  ResultCode = lib.SQLITE_INTERRUPT
	ResultIOErr      ResultCode = lib.SQLITE_IOERR
	ResultCorrupt    ResultCode = lib.SQLITE_CORRUPT
	ResultNotFound   ResultCode = lib.SQLITE_NOTFOUND
	ResultFull       ResultCode = lib.SQLITE_FULL
	ResultCantOpen   ResultCode = lib.SQLITE_CANTOPEN
	ResultProtocol   ResultCode = lib.SQLITE_PROTOCOL
	ResultEmpty      ResultCode = lib.SQLITE_EMPTY
	ResultSchema     ResultCode = lib.SQLITE_SCHEMA
	ResultTooBig     ResultCode = lib.SQLITE_TOOBIG
	ResultConstraint ResultCode = lib.SQLITE_CONSTRAINT
	ResultMismatch   ResultCode = lib.SQLITE_MISMATCH
	ResultMisuse     ResultCode = lib.SQLITE_MISUSE
	ResultNoLFS      ResultCode = lib.SQLITE_NOLFS
	ResultAuth       ResultCode = lib.SQLITE_AUTH
	ResultFormat     ResultCode = lib.SQLITE_FORMAT
	ResultRange      ResultCode = lib.SQLITE_RANGE
	ResultNotADB     ResultCode = lib.SQLITE_NOTADB
	ResultNotice     ResultCode = lib.SQLITE_NOTICE
	ResultWarning    ResultCode = lib.SQLITE_WARNING
	ResultRow        ResultCode = lib.SQLITE_ROW
	ResultDone       ResultCode = lib.SQLITE_DONE
)

// Extended result codes.
const (
	ResultErrorMissingCollSeq    ResultCode = lib.SQLITE_ERROR_MISSING_COLLSEQ
	ResultErrorRetry             ResultCode = lib.SQLITE_ERROR_RETRY
	ResultErrorSnapshot          ResultCode = lib.SQLITE_ERROR_SNAPSHOT
	ResultIOErrRead              ResultCode = lib.SQLITE_IOERR_READ
	ResultIOErrShortRead         ResultCode = lib.SQLITE_IOERR_SHORT_READ
	ResultIOErrWrite             ResultCode = lib.SQLITE_IOERR_WRITE
	ResultIOErrFsync             ResultCode = lib.SQLITE_IOERR_FSYNC
	ResultIOErrDirFsync          ResultCode = lib.SQLITE_IOERR_DIR_FSYNC
	ResultIOErrTruncate          ResultCode = lib.SQLITE_IOERR_TRUNCATE
	ResultIOErrFstat             ResultCode = lib.SQLITE_IOERR_FSTAT
	ResultIOErrUnlock            ResultCode = lib.SQLITE_IOERR_UNLOCK
	ResultIOErrReadLock          ResultCode = lib.SQLITE_IOERR_RDLOCK
	ResultIOErrDelete            ResultCode = lib.SQLITE_IOERR_DELETE
	ResultIOErrBlocked           ResultCode = lib.SQLITE_IOERR_BLOCKED
	ResultIOErrNoMem             ResultCode = lib.SQLITE_IOERR_NOMEM
	ResultIOErrAccess            ResultCode = lib.SQLITE_IOERR_ACCESS
	ResultIOErrCheckReservedLock ResultCode = lib.SQLITE_IOERR_CHECKRESERVEDLOCK
	ResultIOErrLock              ResultCode = lib.SQLITE_IOERR_LOCK
	ResultIOErrClose             ResultCode = lib.SQLITE_IOERR_CLOSE
	ResultIOErrDirClose          ResultCode = lib.SQLITE_IOERR_DIR_CLOSE
	ResultIOErrSHMOpen           ResultCode = lib.SQLITE_IOERR_SHMOPEN
	ResultIOErrSHMSize           ResultCode = lib.SQLITE_IOERR_SHMSIZE
	ResultIOErrSHMLock           ResultCode = lib.SQLITE_IOERR_SHMLOCK
	ResultIOErrSHMMap            ResultCode = lib.SQLITE_IOERR_SHMMAP
	ResultIOErrSeek              ResultCode = lib.SQLITE_IOERR_SEEK
	ResultIOErrDeleteNoEnt       ResultCode = lib.SQLITE_IOERR_DELETE_NOENT
	ResultIOErrMMap              ResultCode = lib.SQLITE_IOERR_MMAP
	ResultIOErrGetTempPath       ResultCode = lib.SQLITE_IOERR_GETTEMPPATH
	ResultIOErrConvPath          ResultCode = lib.SQLITE_IOERR_CONVPATH
	ResultIOErrVNode             ResultCode = lib.SQLITE_IOERR_VNODE
	ResultIOErrAuth              ResultCode = lib.SQLITE_IOERR_AUTH
	ResultIOErrBeginAtomic       ResultCode = lib.SQLITE_IOERR_BEGIN_ATOMIC
	ResultIOErrCommitAtomic      ResultCode = lib.SQLITE_IOERR_COMMIT_ATOMIC
	ResultIOErrRollbackAtomic    ResultCode = lib.SQLITE_IOERR_ROLLBACK_ATOMIC
	ResultLockedSharedCache      ResultCode = lib.SQLITE_LOCKED_SHAREDCACHE
	ResultBusyRecovery           ResultCode = lib.SQLITE_BUSY_RECOVERY
	ResultBusySnapshot           ResultCode = lib.SQLITE_BUSY_SNAPSHOT
	ResultCantOpenNoTempDir      ResultCode = lib.SQLITE_CANTOPEN_NOTEMPDIR
	ResultCantOpenIsDir          ResultCode = lib.SQLITE_CANTOPEN_ISDIR
	ResultCantOpenFullPath       ResultCode = lib.SQLITE_CANTOPEN_FULLPATH
	ResultCantOpenConvPath       ResultCode = lib.SQLITE_CANTOPEN_CONVPATH
	ResultCorruptVTab            ResultCode = lib.SQLITE_CORRUPT_VTAB
	ResultReadOnlyRecovery       ResultCode = lib.SQLITE_READONLY_RECOVERY
	ResultReadOnlyCantLock       ResultCode = lib.SQLITE_READONLY_CANTLOCK
	ResultReadOnlyRollback       ResultCode = lib.SQLITE_READONLY_ROLLBACK
	ResultReadOnlyDBMoved        ResultCode = lib.SQLITE_READONLY_DBMOVED
	ResultReadOnlyCantInit       ResultCode = lib.SQLITE_READONLY_CANTINIT
	ResultReadOnlyDirectory      ResultCode = lib.SQLITE_READONLY_DIRECTORY
	ResultAbortRollback          ResultCode = lib.SQLITE_ABORT_ROLLBACK
	ResultConstraintCheck        ResultCode = lib.SQLITE_CONSTRAINT_CHECK
	ResultConstraintCommitHook   ResultCode = lib.SQLITE_CONSTRAINT_COMMITHOOK
	ResultConstraintForeignKey   ResultCode = lib.SQLITE_CONSTRAINT_FOREIGNKEY
	ResultConstraintFunction     ResultCode = lib.SQLITE_CONSTRAINT_FUNCTION
	ResultConstraintNotNull      ResultCode = lib.SQLITE_CONSTRAINT_NOTNULL
	ResultConstraintPrimaryKey   ResultCode = lib.SQLITE_CONSTRAINT_PRIMARYKEY
	ResultConstraintTrigger      ResultCode = lib.SQLITE_CONSTRAINT_TRIGGER
	ResultConstraintUnique       ResultCode = lib.SQLITE_CONSTRAINT_UNIQUE
	ResultConstraintVTab         ResultCode = lib.SQLITE_CONSTRAINT_VTAB
	ResultConstraintRowID        ResultCode = lib.SQLITE_CONSTRAINT_ROWID
	ResultNoticeRecoverWAL       ResultCode = lib.SQLITE_NOTICE_RECOVER_WAL
	ResultNoticeRecoverRollback  ResultCode = lib.SQLITE_NOTICE_RECOVER_ROLLBACK
	ResultWarningAutoIndex       ResultCode = lib.SQLITE_WARNING_AUTOINDEX
	ResultAuthUser               ResultCode = lib.SQLITE_AUTH_USER
)

// ToPrimary returns the primary result code of the given code.
// https://sqlite.org/rescode.html#primary_result_codes_versus_extended_result_codes
func (code ResultCode) ToPrimary() ResultCode {
	return code & 0xff
}

// IsSuccess reports whether code indicates success.
func (code ResultCode) IsSuccess() bool {
	return code == ResultOK || code == ResultRow || code == ResultDone
}

// String returns the C constant name of the result code.
func (code ResultCode) String() string {
	switch code {
	case ResultOK:
		return "SQLITE_OK"
	case ResultError:
		return "SQLITE_ERROR"
	case ResultInternal:
		return "SQLITE_INTERNAL"
	case ResultPerm:
		return "SQLITE_PERM"
	case ResultAbort:
		return "SQLITE_ABORT"
	case ResultBusy:
		return "SQLITE_BUSY"
	case ResultLocked:
		return "SQLITE_LOCKED"
	case ResultNoMem:
		return "SQLITE_NOMEM"
	case ResultReadOnly:
		return "SQLITE_READONLY"
	case ResultInterrupt:
		return "SQLITE_INTERRUPT"
	case ResultIOErr:
		return "SQLITE_IOERR"
	case ResultCorrupt:
		return "SQLITE_CORRUPT"
	case ResultNotFound:
		return "SQLITE_NOTFOUND"
	case ResultFull:
		return "SQLITE_FULL"
	case ResultCantOpen:
		return "SQLITE_CANTOPEN"
	case ResultProtocol:
		return "SQLITE_PROTOCOL"
	case ResultEmpty:
		return "SQLITE_EMPTY"
	case ResultSchema:
		return "SQLITE_SCHEMA"
	case ResultTooBig:
		return "SQLITE_TOOBIG"
	case ResultConstraint:
		return "SQLITE_CONSTRAINT"
	case ResultMismatch:
		return "SQLITE_MISMATCH"
	case ResultMisuse:
		return "SQLITE_MISUSE"
	case ResultNoLFS:
		return "SQLITE_NOLFS"
	case ResultAuth:
		return "SQLITE_AUTH"
	case ResultFormat:
		return "SQLITE_FORMAT"
	case ResultRange:
		return "SQLITE_RANGE"
	case ResultNotADB:
		return "SQLITE_NOTADB"
	case ResultNotice:
		return "SQLITE_NOTICE"
	case ResultWarning:
		return "SQLITE_WARNING"
	case ResultRow:
		return "SQLITE_ROW"
	case ResultDone:
		return "SQLITE_DONE"

	case ResultErrorMissingCollSeq:
		return "SQLITE_ERROR_MISSING_COLLSEQ"
	case ResultErrorRetry:
		return "SQLITE_ERROR_RETRY"
	case ResultErrorSnapshot:
		return "SQLITE_ERROR_SNAPSHOT"
	case ResultIOErrRead:
		return "SQLITE_IOERR_READ"
	case ResultIOErrShortRead:
		return "SQLITE_IOERR_SHORT_READ"
	case ResultIOErrWrite:
		return "SQLITE_IOERR_WRITE"
	case ResultIOErrFsync:
		return "SQLITE_IOERR_FSYNC"
	case ResultIOErrDirFsync:
		return "SQLITE_IOERR_DIR_FSYNC"
	case ResultIOErrTruncate:
		return "SQLITE_IOERR_TRUNCATE"
	case ResultIOErrFstat:
		return "SQLITE_IOERR_FSTAT"
	case ResultIOErrUnlock:
		return "SQLITE_IOERR_UNLOCK"
	case ResultIOErrReadLock:
		return "SQLITE_IOERR_RDLOCK"
	case ResultIOErrDelete:
		return "SQLITE_IOERR_DELETE"
	case ResultIOErrBlocked:
		return "SQLITE_IOERR_BLOCKED"
	case ResultIOErrNoMem:
		return "SQLITE_IOERR_NOMEM"
	case ResultIOErrAccess:
		return "SQLITE_IOERR_ACCESS"
	case ResultIOErrCheckReservedLock:
		return "SQLITE_IOERR_CHECKRESERVEDLOCK"
	case ResultIOErrLock:
		return "SQLITE_IOERR_LOCK"
	case ResultIOErrClose:
		return "SQLITE_IOERR_CLOSE"
	case ResultIOErrDirClose:
		return "SQLITE_IOERR_DIR_CLOSE"
	case ResultIOErrSHMOpen:
		return "SQLITE_IOERR_SHMOPEN"
	case ResultIOErrSHMSize:
		return "SQLITE_IOERR_SHMSIZE"
	case ResultIOErrSHMLock:
		return "SQLITE_IOERR_SHMLOCK"
	case ResultIOErrSHMMap:
		return "SQLITE_IOERR_SHMMAP"
	case ResultIOErrSeek:
		return "SQLITE_IOERR_SEEK"
	case ResultIOErrDeleteNoEnt:
		return "SQLITE_IOERR_DELETE_NOENT"
	case ResultIOErrMMap:
		return "SQLITE_IOERR_MMAP"
	case ResultIOErrGetTempPath:
		return "SQLITE_IOERR_GETTEMPPATH"
	case ResultIOErrConvPath:
		return "SQLITE_IOERR_CONVPATH"
	case ResultIOErrVNode:
		return "SQLITE_IOERR_VNODE"
	case ResultIOErrAuth:
		return "SQLITE_IOERR_AUTH"
	case ResultIOErrBeginAtomic:
		return "SQLITE_IOERR_BEGIN_ATOMIC"
	case ResultIOErrCommitAtomic:
		return "SQLITE_IOERR_COMMIT_ATOMIC"
	case ResultIOErrRollbackAtomic:
		return "SQLITE_IOERR_ROLLBACK_ATOMIC"
	case ResultLockedSharedCache:
		return "SQLITE_LOCKED_SHAREDCACHE"
	case ResultBusyRecovery:
		return "SQLITE_BUSY_RECOVERY"
	case ResultBusySnapshot:
		return "SQLITE_BUSY_SNAPSHOT"
	case ResultCantOpenNoTempDir:
		return "SQLITE_CANTOPEN_NOTEMPDIR"
	case ResultCantOpenIsDir:
		return "SQLITE_CANTOPEN_ISDIR"
	case ResultCantOpenFullPath:
		return "SQLITE_CANTOPEN_FULLPATH"
	case ResultCantOpenConvPath:
		return "SQLITE_CANTOPEN_CONVPATH"
	case ResultCorruptVTab:
		return "SQLITE_CORRUPT_VTAB"
	case ResultReadOnlyRecovery:
		return "SQLITE_READONLY_RECOVERY"
	case ResultReadOnlyCantLock:
		return "SQLITE_READONLY_CANTLOCK"
	case ResultReadOnlyRollback:
		return "SQLITE_READONLY_ROLLBACK"
	case ResultReadOnlyDBMoved:
		return "SQLITE_READONLY_DBMOVED"
	case ResultReadOnlyCantInit:
		return "SQLITE_READONLY_CANTINIT"
	case ResultReadOnlyDirectory:
		return "SQLITE_READONLY_DIRECTORY"
	case ResultAbortRollback:
		return "SQLITE_ABORT_ROLLBACK"
	case ResultConstraintCheck:
		return "SQLITE_CONSTRAINT_CHECK"
	case ResultConstraintCommitHook:
		return "SQLITE_CONSTRAINT_COMMITHOOK"
	case ResultConstraintForeignKey:
		return "SQLITE_CONSTRAINT_FOREIGNKEY"
	case ResultConstraintFunction:
		return "SQLITE_CONSTRAINT_FUNCTION"
	case ResultConstraintNotNull:
		return "SQLITE_CONSTRAINT_NOTNULL"
	case ResultConstraintPrimaryKey:
		return "SQLITE_CONSTRAINT_PRIMARYKEY"
	case ResultConstraintTrigger:
		return "SQLITE_CONSTRAINT_TRIGGER"
	case ResultConstraintUnique:
		return "SQLITE_CONSTRAINT_UNIQUE"
	case ResultConstraintVTab:
		return "SQLITE_CONSTRAINT_VTAB"
	case ResultConstraintRowID:
		return "SQLITE_CONSTRAINT_ROWID"
	case ResultNoticeRecoverWAL:
		return "SQLITE_NOTICE_RECOVER_WAL"
	case ResultNoticeRecoverRollback:
		return "SQLITE_NOTICE_RECOVER_ROLLBACK"
	case ResultWarningAutoIndex:
		return "SQLITE_WARNING_AUTOINDEX"
	case ResultAuthUser:
		return "SQLITE_AUTH_USER"
	default:
		return fmt.Sprintf("ResultCode(%d)", int32(code))
	}
}

// Message returns the English-language text that describes the result code.
func (code ResultCode) Message() string {
	tls := libc.NewTLS()
	defer tls.Close()
	initlib(tls)
	cstr := lib.Xsqlite3_errstr(tls, int32(code))
	return libc.GoString(cstr)
}

// ToError converts an error code into an error
// for which ErrCode(code.ToError()) == code.
// If the code indicates success, ToError returns nil.
func (code ResultCode) ToError() error {
	if code.IsSuccess() {
		return nil
	}
	return sqliteError{code}
}

type sqliteError struct {
	code ResultCode
}

func (e sqliteError) Error() string {
	return e.code.Message()
}

type extendedError struct {
	code    ResultCode
	offset  int // negative if not present
	message string
}

func (e *extendedError) Error() string {
	m := e.code.Message()
	if e.message == "" || e.message == m {
		// Only use the connection's message if it's adding more information.
		// Sometimes the connection will return the code's message.
		return m
	}
	return m + ": " + e.message
}

func (e *extendedError) As(target any) bool {
	switch target := target.(type) {
	case *sqliteError:
		*target = sqliteError{e.code}
		return true
	case **extendedError:
		*target = e
		return true
	default:
		return false
	}
}

// ErrCode returns the error's SQLite error code
// or [ResultError] if the error does not represent a SQLite error.
// ErrCode returns [ResultOK] if and only if the error is nil.
func ErrCode(err error) ResultCode {
	if err == nil {
		return ResultOK
	}
	if e := new(sqliteError); errors.As(err, e) {
		return e.code
	}
	return ResultError
}

// ErrorOffset returns the byte offset of the start of the SQL token
// that the error references if known.
func ErrorOffset(err error) (offset int, ok bool) {
	if e := (*extendedError)(nil); errors.As(err, &e) {
		return e.offset, e.offset >= 0
	}
	return -1, false
}
