// Copyright (c) 2018 David Crawshaw <david@zentus.com>
// Copyright (c) 2021 Roxy Light <roxy@zombiezen.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
//
// SPDX-License-Identifier: ISC

package sqlitex

import (
	"fmt"
	"runtime"
	"strings"

	"zombiezen.com/go/sqlite"
)

// Save creates a named SQLite transaction using SAVEPOINT.
//
// On success Savepoint returns a releaseFn that will call either
// RELEASE or ROLLBACK depending on whether the parameter *error
// points to a nil or non-nil error. This is designed to be deferred.
//
// https://www.sqlite.org/lang_savepoint.html
func Save(conn *sqlite.Conn) (releaseFn func(*error)) {
	name := "sqlitex.Save" // safe as names can be reused
	var pc [3]uintptr
	if n := runtime.Callers(0, pc[:]); n > 0 {
		frames := runtime.CallersFrames(pc[:n])
		if _, more := frames.Next(); more { // runtime.Callers
			if _, more := frames.Next(); more { // savepoint.Save
				frame, _ := frames.Next() // caller we care about
				if frame.Function != "" {
					name = frame.Function
				}
			}
		}
	}

	releaseFn, err := savepoint(conn, name)
	if err != nil {
		if sqlite.ErrCode(err) == sqlite.ResultInterrupt {
			return func(errp *error) {
				if *errp == nil {
					*errp = err
				}
			}
		}
		panic(err)
	}
	return releaseFn
}

func savepoint(conn *sqlite.Conn, name string) (releaseFn func(*error), err error) {
	if strings.Contains(name, `"`) {
		return nil, fmt.Errorf("sqlitex.Savepoint: invalid name: %q", name)
	}
	if err := Execute(conn, fmt.Sprintf("SAVEPOINT %q;", name), nil); err != nil {
		return nil, err
	}
	releaseFn = func(errp *error) {
		recoverP := recover()

		// If a query was interrupted or if a user exec'd COMMIT or
		// ROLLBACK, then everything was already rolled back
		// automatically, thus returning the connection to autocommit
		// mode.
		if conn.AutocommitEnabled() {
			// There is nothing to rollback.
			if recoverP != nil {
				panic(recoverP)
			}
			return
		}

		if *errp == nil && recoverP == nil {
			// Success path. Release the savepoint successfully.
			*errp = Execute(conn, fmt.Sprintf("RELEASE %q;", name), nil)
			if *errp == nil {
				return
			}
			// Possible interrupt. Fall through to the error path.
			if conn.AutocommitEnabled() {
				// There is nothing to rollback.
				if recoverP != nil {
					panic(recoverP)
				}
				return
			}
		}

		orig := ""
		if *errp != nil {
			orig = (*errp).Error() + "\n\t"
		}

		// Error path.

		// Always run ROLLBACK even if the connection has been interrupted.
		oldDoneCh := conn.SetInterrupt(nil)
		defer conn.SetInterrupt(oldDoneCh)

		err := Execute(conn, fmt.Sprintf("ROLLBACK TO %q;", name), nil)
		if err != nil {
			panic(orig + err.Error())
		}
		err = Execute(conn, fmt.Sprintf("RELEASE %q;", name), nil)
		if err != nil {
			panic(orig + err.Error())
		}

		if recoverP != nil {
			panic(recoverP)
		}
	}
	return releaseFn, nil
}

// Transaction creates a DEFERRED SQLite transaction.
//
// On success Transaction returns an endFn that will call either
// COMMIT or ROLLBACK depending on whether the parameter *error
// points to a nil or non-nil error. This is designed to be deferred.
//
// https://www.sqlite.org/lang_transaction.html
func Transaction(conn *sqlite.Conn) (endFn func(*error)) {
	endFn, err := transaction(conn, "DEFERRED")
	if err != nil {
		if sqlite.ErrCode(err) == sqlite.ResultInterrupt {
			return func(errp *error) {
				if *errp == nil {
					*errp = err
				}
			}
		}
		panic(err)
	}
	return endFn
}

// ImmediateTransaction creates an IMMEDIATE SQLite transaction.
//
// On success ImmediateTransaction returns an endFn that will call either
// COMMIT or ROLLBACK depending on whether the parameter *error
// points to a nil or non-nil error. This is designed to be deferred.
//
// https://www.sqlite.org/lang_transaction.html
func ImmediateTransaction(conn *sqlite.Conn) (endFn func(*error), err error) {
	endFn, err = transaction(conn, "IMMEDIATE")
	if err != nil {
		return func(*error) {}, err
	}
	return endFn, nil
}

// ExclusiveTransaction creates an EXCLUSIVE SQLite transaction.
//
// On success ImmediateTransaction returns an endFn that will call either
// COMMIT or ROLLBACK depending on whether the parameter *error
// points to a nil or non-nil error. This is designed to be deferred.
//
// https://www.sqlite.org/lang_transaction.html
func ExclusiveTransaction(conn *sqlite.Conn) (endFn func(*error), err error) {
	endFn, err = transaction(conn, "EXCLUSIVE")
	if err != nil {
		return func(*error) {}, err
	}
	return endFn, nil
}

func transaction(conn *sqlite.Conn, mode string) (endFn func(*error), err error) {
	if err := Execute(conn, "BEGIN "+mode+";", nil); err != nil {
		return nil, err
	}
	endFn = func(errp *error) {
		recoverP := recover()

		// If a query was interrupted or if a user exec'd COMMIT or
		// ROLLBACK, then everything was already rolled back
		// automatically, thus returning the connection to autocommit
		// mode.
		if conn.AutocommitEnabled() {
			// There is nothing to rollback.
			if recoverP != nil {
				panic(recoverP)
			}
			return
		}

		if *errp == nil && recoverP == nil {
			// Success path. Commit the transaction.
			*errp = Execute(conn, "COMMIT;", nil)
			if *errp == nil {
				return
			}
			// Possible interrupt. Fall through to the error path.
			if conn.AutocommitEnabled() {
				// There is nothing to rollback.
				if recoverP != nil {
					panic(recoverP)
				}
				return
			}
		}

		orig := ""
		if *errp != nil {
			orig = (*errp).Error() + "\n\t"
		}

		// Error path.

		// Always run ROLLBACK even if the connection has been interrupted.
		oldDoneCh := conn.SetInterrupt(nil)
		defer conn.SetInterrupt(oldDoneCh)

		err := Execute(conn, "ROLLBACK;", nil)
		if err != nil {
			panic(orig + err.Error())
		}

		if recoverP != nil {
			panic(recoverP)
		}
	}
	return endFn, nil
}
