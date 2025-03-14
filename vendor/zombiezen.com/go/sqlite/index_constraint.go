// Copyright 2023 Roxy Light
// SPDX-License-Identifier: ISC

package sqlite

import (
	"fmt"
	"unsafe"

	"modernc.org/libc"
	lib "modernc.org/sqlite/lib"
)

// IndexConstraint is a constraint term in the WHERE clause
// of a query that uses a virtual table.
type IndexConstraint struct {
	// Column is the left-hand operand.
	// Column indices start at 0.
	// -1 indicates the left-hand operand is the rowid.
	// Column should be ignored when Op is [IndexConstraintLimit] or [IndexConstraintOffset].
	Column int
	// Op is the constraint's operator.
	Op IndexConstraintOp
	// Usable indicates whether BestIndex should consider the constraint.
	// Usable may false depending on how tables are ordered in a join.
	Usable bool
	// Collation is the name of the collating sequence
	// that should be used when evaluating the constraint.
	Collation string
	// RValue is the right-hand operand, if known during statement preparation.
	// It's only valid until the end of BestIndex.
	RValue Value
	// RValueKnown indicates whether RValue is set.
	RValueKnown bool
}

func (c *IndexConstraint) copyFromC(tls *libc.TLS, infoPtr uintptr, i int32, ppVal uintptr) {
	info := (*lib.Sqlite3_index_info)(unsafe.Pointer(infoPtr))
	src := (*lib.Sqlite3_index_constraint)(unsafe.Pointer(info.FaConstraint + uintptr(i)*unsafe.Sizeof(lib.Sqlite3_index_constraint{})))
	*c = IndexConstraint{
		Column: int(src.FiColumn),
		Op:     IndexConstraintOp(src.Fop),
		Usable: src.Fusable != 0,
	}

	const binaryCollation = "BINARY"
	cCollation := lib.Xsqlite3_vtab_collation(tls, infoPtr, int32(i))
	if isCStringEqual(cCollation, binaryCollation) {
		// BINARY is the most common, so avoid allocations in this case.
		c.Collation = binaryCollation
	} else {
		c.Collation = libc.GoString(cCollation)
	}

	if ppVal != 0 {
		res := ResultCode(lib.Xsqlite3_vtab_rhs_value(tls, infoPtr, int32(i), ppVal))
		if res == ResultOK {
			c.RValue = Value{
				tls:       tls,
				ptrOrType: *(*uintptr)(unsafe.Pointer(ppVal)),
			}
			c.RValueKnown = true
		}
	}
}

// IndexConstraintOp is an enumeration of virtual table constraint operators
// used in [IndexConstraint].
type IndexConstraintOp uint8

const (
	IndexConstraintEq        IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_EQ
	IndexConstraintGT        IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_GT
	IndexConstraintLE        IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_LE
	IndexConstraintLT        IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_LT
	IndexConstraintGE        IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_GE
	IndexConstraintMatch     IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_MATCH
	IndexConstraintLike      IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_LIKE
	IndexConstraintGlob      IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_GLOB
	IndexConstraintRegexp    IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_REGEXP
	IndexConstraintNE        IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_NE
	IndexConstraintIsNot     IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_ISNOT
	IndexConstraintIsNotNull IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_ISNOTNULL
	IndexConstraintIsNull    IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_ISNULL
	IndexConstraintIs        IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_IS
	IndexConstraintLimit     IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_LIMIT
	IndexConstraintOffset    IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_OFFSET
)

const indexConstraintFunction IndexConstraintOp = lib.SQLITE_INDEX_CONSTRAINT_FUNCTION

// String returns the operator symbol or keyword.
func (op IndexConstraintOp) String() string {
	switch op {
	case IndexConstraintEq:
		return "="
	case IndexConstraintGT:
		return ">"
	case IndexConstraintLE:
		return "<="
	case IndexConstraintLT:
		return "<"
	case IndexConstraintGE:
		return ">="
	case IndexConstraintMatch:
		return "MATCH"
	case IndexConstraintLike:
		return "LIKE"
	case IndexConstraintGlob:
		return "GLOB"
	case IndexConstraintRegexp:
		return "REGEXP"
	case IndexConstraintNE:
		return "<>"
	case IndexConstraintIsNot:
		return "IS NOT"
	case IndexConstraintIsNotNull:
		return "IS NOT NULL"
	case IndexConstraintIsNull:
		return "IS NULL"
	case IndexConstraintIs:
		return "IS"
	case IndexConstraintLimit:
		return "LIMIT"
	case IndexConstraintOffset:
		return "OFFSET"
	default:
		if op < indexConstraintFunction {
			return fmt.Sprintf("IndexConstraintOp(%d)", uint8(op))
		}
		return fmt.Sprintf("<function %d>", uint8(op))
	}
}

func isCStringEqual(c uintptr, s string) bool {
	if c == 0 {
		return s == ""
	}
	for {
		cc := *(*byte)(unsafe.Pointer(c))
		if cc == 0 {
			return len(s) == 0
		}
		if len(s) == 0 || cc != s[0] {
			return false
		}
		c++
		s = s[1:]
	}
}
