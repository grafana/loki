// +build go1.8

/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to you under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package avatica

import (
	"io"
	"reflect"
)

func (r *rows) HasNextResultSet() bool {
	lastResultSetID := len(r.resultSets) - 1
	return lastResultSetID > r.currentResultSet
}

func (r *rows) NextResultSet() error {

	lastResultSetID := len(r.resultSets) - 1

	if r.currentResultSet+1 > lastResultSetID {
		return io.EOF
	}

	r.currentResultSet++

	return nil
}

func (r *rows) ColumnTypeDatabaseTypeName(index int) string {

	return r.resultSets[r.currentResultSet].columns[index].TypeName
}

func (r *rows) ColumnTypeLength(index int) (length int64, ok bool) {
	l := r.resultSets[r.currentResultSet].columns[index].Length

	if l == 0 {
		return 0, false
	}

	return l, true
}

func (r *rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	return r.resultSets[r.currentResultSet].columns[index].Nullable, true
}

func (r *rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {

	ps := r.resultSets[r.currentResultSet].columns[index].PrecisionScale

	if ps != nil {
		return ps.Precision, ps.Scale, true
	}

	return 0, 0, false
}

func (r *rows) ColumnTypeScanType(index int) reflect.Type {
	return r.resultSets[r.currentResultSet].columns[index].ScanType
}
