// Copyright 2021 Roxy Light
// SPDX-License-Identifier: ISC

package sqlitex

import (
	"errors"

	"zombiezen.com/go/sqlite"
)

var (
	errNoResults       = errors.New("sqlite: statement has no results")
	errMultipleResults = errors.New("sqlite: statement has multiple result rows")
)

func resultSetup(stmt *sqlite.Stmt) error {
	hasRow, err := stmt.Step()
	if err != nil {
		stmt.Reset()
		return err
	}
	if !hasRow {
		stmt.Reset()
		return errNoResults
	}
	return nil
}

func resultTeardown(stmt *sqlite.Stmt) error {
	hasRow, err := stmt.Step()
	if err != nil {
		stmt.Reset()
		return err
	}
	if hasRow {
		stmt.Reset()
		return errMultipleResults
	}
	return stmt.Reset()
}

// ResultBool reports whether the first column of the first and only row
// produced by running stmt
// is non-zero.
// It returns an error if there is not exactly one result row.
func ResultBool(stmt *sqlite.Stmt) (bool, error) {
	res, err := ResultInt64(stmt)
	return res != 0, err
}

// ResultInt returns the first column of the first and only row
// produced by running stmt
// as an integer.
// It returns an error if there is not exactly one result row.
func ResultInt(stmt *sqlite.Stmt) (int, error) {
	res, err := ResultInt64(stmt)
	return int(res), err
}

// ResultInt64 returns the first column of the first and only row
// produced by running stmt
// as an integer.
// It returns an error if there is not exactly one result row.
func ResultInt64(stmt *sqlite.Stmt) (int64, error) {
	if err := resultSetup(stmt); err != nil {
		return 0, err
	}
	res := stmt.ColumnInt64(0)
	if err := resultTeardown(stmt); err != nil {
		return 0, err
	}
	return res, nil
}

// ResultText returns the first column of the first and only row
// produced by running stmt
// as text.
// It returns an error if there is not exactly one result row.
func ResultText(stmt *sqlite.Stmt) (string, error) {
	if err := resultSetup(stmt); err != nil {
		return "", err
	}
	res := stmt.ColumnText(0)
	if err := resultTeardown(stmt); err != nil {
		return "", err
	}
	return res, nil
}

// ResultFloat returns the first column of the first and only row
// produced by running stmt
// as a real number.
// It returns an error if there is not exactly one result row.
func ResultFloat(stmt *sqlite.Stmt) (float64, error) {
	if err := resultSetup(stmt); err != nil {
		return 0, err
	}
	res := stmt.ColumnFloat(0)
	if err := resultTeardown(stmt); err != nil {
		return 0, err
	}
	return res, nil
}

// ResultBytes reads the first column of the first and only row
// produced by running stmt into buf,
// returning the number of bytes read.
// It returns an error if there is not exactly one result row.
func ResultBytes(stmt *sqlite.Stmt, buf []byte) (int, error) {
	if err := resultSetup(stmt); err != nil {
		return 0, err
	}
	read := stmt.ColumnBytes(0, buf)
	if err := resultTeardown(stmt); err != nil {
		return 0, err
	}
	return read, nil
}
