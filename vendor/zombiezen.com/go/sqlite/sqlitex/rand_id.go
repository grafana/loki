// Copyright (c) 2019 David Crawshaw <david@zentus.com>
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
	"crypto/rand"
	"fmt"
	"math/big"

	"zombiezen.com/go/sqlite"
)

// InsertRandID executes stmt with a random value in the range [min, max) for $param.
func InsertRandID(stmt *sqlite.Stmt, param string, min, max int64) (int64, error) {
	if min < 0 {
		return 0, fmt.Errorf("sqlitex.InsertRandID: min (%d) is negative", min)
	}

	for i := 0; ; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(max-min))
		if err != nil {
			return 0, fmt.Errorf("sqlitex.InsertRandID: %w", err)
		}
		id := v.Int64() + min

		stmt.Reset()
		stmt.SetInt64(param, id)
		_, err = stmt.Step()
		if err == nil {
			return id, nil
		}
		if i >= 100 || sqlite.ErrCode(err) != sqlite.ResultConstraintPrimaryKey {
			return 0, fmt.Errorf("sqlitex.InsertRandID: %w", err)
		}
	}
}
