// Copyright 2021 Roxy Light
// SPDX-License-Identifier: ISC

package sqlite

import (
	"fmt"

	lib "modernc.org/sqlite/lib"
)

// OpType is an enumeration of SQLite statements and authorizable actions.
type OpType int32

// Operation types
const (
	OpCreateIndex       OpType = lib.SQLITE_CREATE_INDEX
	OpCreateTable       OpType = lib.SQLITE_CREATE_TABLE
	OpCreateTempIndex   OpType = lib.SQLITE_CREATE_TEMP_INDEX
	OpCreateTempTable   OpType = lib.SQLITE_CREATE_TEMP_TABLE
	OpCreateTempTrigger OpType = lib.SQLITE_CREATE_TEMP_TRIGGER
	OpCreateTempView    OpType = lib.SQLITE_CREATE_TEMP_VIEW
	OpCreateTrigger     OpType = lib.SQLITE_CREATE_TRIGGER
	OpCreateView        OpType = lib.SQLITE_CREATE_VIEW
	OpDelete            OpType = lib.SQLITE_DELETE
	OpDropIndex         OpType = lib.SQLITE_DROP_INDEX
	OpDropTable         OpType = lib.SQLITE_DROP_TABLE
	OpDropTempIndex     OpType = lib.SQLITE_DROP_TEMP_INDEX
	OpDropTempTable     OpType = lib.SQLITE_DROP_TEMP_TABLE
	OpDropTempTrigger   OpType = lib.SQLITE_DROP_TEMP_TRIGGER
	OpDropTempView      OpType = lib.SQLITE_DROP_TEMP_VIEW
	OpDropTrigger       OpType = lib.SQLITE_DROP_TRIGGER
	OpDropView          OpType = lib.SQLITE_DROP_VIEW
	OpInsert            OpType = lib.SQLITE_INSERT
	OpPragma            OpType = lib.SQLITE_PRAGMA
	OpRead              OpType = lib.SQLITE_READ
	OpSelect            OpType = lib.SQLITE_SELECT
	OpTransaction       OpType = lib.SQLITE_TRANSACTION
	OpUpdate            OpType = lib.SQLITE_UPDATE
	OpAttach            OpType = lib.SQLITE_ATTACH
	OpDetach            OpType = lib.SQLITE_DETACH
	OpAlterTable        OpType = lib.SQLITE_ALTER_TABLE
	OpReindex           OpType = lib.SQLITE_REINDEX
	OpAnalyze           OpType = lib.SQLITE_ANALYZE
	OpCreateVTable      OpType = lib.SQLITE_CREATE_VTABLE
	OpDropVTable        OpType = lib.SQLITE_DROP_VTABLE
	OpFunction          OpType = lib.SQLITE_FUNCTION
	OpSavepoint         OpType = lib.SQLITE_SAVEPOINT
	OpCopy              OpType = lib.SQLITE_COPY
	OpRecursive         OpType = lib.SQLITE_RECURSIVE
)

// String returns the C constant name of the operation type.
func (op OpType) String() string {
	switch op {
	case OpCreateIndex:
		return "SQLITE_CREATE_INDEX"
	case OpCreateTable:
		return "SQLITE_CREATE_TABLE"
	case OpCreateTempIndex:
		return "SQLITE_CREATE_TEMP_INDEX"
	case OpCreateTempTable:
		return "SQLITE_CREATE_TEMP_TABLE"
	case OpCreateTempTrigger:
		return "SQLITE_CREATE_TEMP_TRIGGER"
	case OpCreateTempView:
		return "SQLITE_CREATE_TEMP_VIEW"
	case OpCreateTrigger:
		return "SQLITE_CREATE_TRIGGER"
	case OpCreateView:
		return "SQLITE_CREATE_VIEW"
	case OpDelete:
		return "SQLITE_DELETE"
	case OpDropIndex:
		return "SQLITE_DROP_INDEX"
	case OpDropTable:
		return "SQLITE_DROP_TABLE"
	case OpDropTempIndex:
		return "SQLITE_DROP_TEMP_INDEX"
	case OpDropTempTable:
		return "SQLITE_DROP_TEMP_TABLE"
	case OpDropTempTrigger:
		return "SQLITE_DROP_TEMP_TRIGGER"
	case OpDropTempView:
		return "SQLITE_DROP_TEMP_VIEW"
	case OpDropTrigger:
		return "SQLITE_DROP_TRIGGER"
	case OpDropView:
		return "SQLITE_DROP_VIEW"
	case OpInsert:
		return "SQLITE_INSERT"
	case OpPragma:
		return "SQLITE_PRAGMA"
	case OpRead:
		return "SQLITE_READ"
	case OpSelect:
		return "SQLITE_SELECT"
	case OpTransaction:
		return "SQLITE_TRANSACTION"
	case OpUpdate:
		return "SQLITE_UPDATE"
	case OpAttach:
		return "SQLITE_ATTACH"
	case OpDetach:
		return "SQLITE_DETACH"
	case OpAlterTable:
		return "SQLITE_ALTER_TABLE"
	case OpReindex:
		return "SQLITE_REINDEX"
	case OpAnalyze:
		return "SQLITE_ANALYZE"
	case OpCreateVTable:
		return "SQLITE_CREATE_VTABLE"
	case OpDropVTable:
		return "SQLITE_DROP_VTABLE"
	case OpFunction:
		return "SQLITE_FUNCTION"
	case OpSavepoint:
		return "SQLITE_SAVEPOINT"
	case OpCopy:
		return "SQLITE_COPY"
	case OpRecursive:
		return "SQLITE_RECURSIVE"
	default:
		return fmt.Sprintf("OpType(%d)", int32(op))
	}
}
