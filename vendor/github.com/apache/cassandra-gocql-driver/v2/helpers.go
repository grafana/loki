/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * Content before git sha 34fdeebefcbf183ed7f916f931aa0586fdaa1b40
 * Copyright (c) 2012, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
)

// RowData contains the column names and pointers to the default values for each
// column
type RowData struct {
	Columns []string
	Values  []interface{}
}

func dereference(i interface{}) interface{} {
	return reflect.Indirect(reflect.ValueOf(i)).Interface()
}

// TupleColumnName will return the column name of a tuple value in a column named
// c at index n. It should be used if a specific element within a tuple is needed
// to be extracted from a map returned from SliceMap or MapScan.
func TupleColumnName(c string, n int) string {
	return fmt.Sprintf("%s[%d]", c, n)
}

// RowData returns the RowData for the iterator.
func (iter *Iter) RowData() (RowData, error) {
	if iter.err != nil {
		return RowData{}, iter.err
	}

	columns := make([]string, 0, len(iter.Columns()))
	values := make([]interface{}, 0, len(iter.Columns()))

	for _, column := range iter.Columns() {
		if c, ok := column.TypeInfo.(TupleTypeInfo); !ok {
			val := c.Zero()
			columns = append(columns, column.Name)
			values = append(values, &val)
		} else {
			for i, elem := range c.Elems {
				columns = append(columns, TupleColumnName(column.Name, i))
				var val interface{}
				val = elem.Zero()
				values = append(values, &val)
			}
		}
	}

	rowData := RowData{
		Columns: columns,
		Values:  values,
	}

	return rowData, nil
}

// SliceMap is a helper function to make the API easier to use.
// It returns the data from the query in the form of []map[string]interface{}.
//
// Columns are automatically converted to Go types based on their CQL type.
// The following table shows exactly what Go type to expect when accessing map values:
//
//	CQL Type             | Go Type (Non-NULL)   | Go Value for NULL    | Type Assertion Example
//	ascii                | string               | ""                   | row["col"].(string)
//	bigint               | int64                | int64(0)             | row["col"].(int64)
//	blob                 | []byte               | []byte(nil)          | row["col"].([]byte)
//	boolean              | bool                 | false                | row["col"].(bool)
//	counter              | int64                | int64(0)             | row["col"].(int64)
//	date                 | time.Time            | time.Time{}          | row["col"].(time.Time)
//	decimal              | *inf.Dec             | (*inf.Dec)(nil)      | row["col"].(*inf.Dec)
//	double               | float64              | float64(0)           | row["col"].(float64)
//	duration             | gocql.Duration       | gocql.Duration{}     | row["col"].(gocql.Duration)
//	float                | float32              | float32(0)           | row["col"].(float32)
//	inet                 | net.IP               | net.IP(nil)          | row["col"].(net.IP)
//	int                  | int                  | int(0)               | row["col"].(int)
//	list<T>              | []T                  | []T(nil)             | row["col"].([]string)
//	map<K,V>             | map[K]V              | map[K]V(nil)         | row["col"].(map[string]int)
//	set<T>               | []T                  | []T(nil)             | row["col"].([]int)
//	smallint             | int16                | int16(0)             | row["col"].(int16)
//	text                 | string               | ""                   | row["col"].(string)
//	time                 | time.Duration        | time.Duration(0)     | row["col"].(time.Duration)
//	timestamp            | time.Time            | time.Time{}          | row["col"].(time.Time)
//	timeuuid             | gocql.UUID           | gocql.UUID{}         | row["col"].(gocql.UUID)
//	tinyint              | int8                 | int8(0)              | row["col"].(int8)
//	tuple<T1,T2,...>     | (see below)          | (see below)          | (see below)
//	uuid                 | gocql.UUID           | gocql.UUID{}         | row["col"].(gocql.UUID)
//	varchar              | string               | ""                   | row["col"].(string)
//	varint               | *big.Int             | (*big.Int)(nil)      | row["col"].(*big.Int)
//	vector<T,N>          | []T                  | []T(nil)             | row["col"].([]float32)
//
// Special Cases:
//
// Tuple Types: Tuple elements are split into separate map entries with keys like "column[0]", "column[1]", etc.
// Use TupleColumnName to generate the correct key:
//
//	// For tuple<int, text> column named "my_tuple"
//	elem0 := row[gocql.TupleColumnName("my_tuple", 0)].(int)
//	elem1 := row[gocql.TupleColumnName("my_tuple", 1)].(string)
//
// User-Defined Types (UDTs): Returned as map[string]interface{} with field names as keys:
//
//	udt := row["my_udt"].(map[string]interface{})
//	name := udt["name"].(string)
//	age := udt["age"].(int)
//
// Important Notes:
//   - Always use type assertions when accessing map values: row["col"].(ExpectedType)
//   - NULL database values return Go zero values or nil for pointer types
//   - Collection types (list, set, map, vector) return nil slices/maps for NULL values
//   - Migration from v1.x: inet columns now return net.IP instead of string values
func (iter *Iter) SliceMap() ([]map[string]interface{}, error) {
	if iter.err != nil {
		return nil, iter.err
	}

	numCols := len(iter.Columns())
	var dataToReturn []map[string]interface{}
	for {
		m := make(map[string]interface{}, numCols)
		if !iter.MapScan(m) {
			break
		}
		dataToReturn = append(dataToReturn, m)
	}
	if iter.err != nil {
		return nil, iter.err
	}
	return dataToReturn, nil
}

// MapScan takes a map[string]interface{} and populates it with a row
// that is returned from Cassandra.
//
// Each call to MapScan() must be called with a new map object.
// During the call to MapScan() any pointers in the existing map
// are replaced with non pointer types before the call returns.
//
// Columns are automatically converted to Go types based on their CQL type.
// See SliceMap for the complete CQL to Go type mapping table and examples.
//
// Usage Examples:
//
//	iter := session.Query(`SELECT * FROM mytable`).Iter()
//	for {
//		// New map each iteration
//		row := make(map[string]interface{})
//		if !iter.MapScan(row) {
//			break
//		}
//		// Do things with row
//		if fullname, ok := row["fullname"]; ok {
//			fmt.Printf("Full Name: %s\n", fullname)
//		}
//	}
//
// You can also pass pointers in the map before each call:
//
//	var fullName FullName // Implements gocql.Unmarshaler and gocql.Marshaler interfaces
//	var address net.IP
//	var age int
//	iter := session.Query(`SELECT * FROM scan_map_table`).Iter()
//	for {
//		// New map each iteration
//		row := map[string]interface{}{
//			"fullname": &fullName,
//			"age":      &age,
//			"address":  &address,
//		}
//		if !iter.MapScan(row) {
//			break
//		}
//		fmt.Printf("First: %s Age: %d Address: %q\n", fullName.FirstName, age, address)
//	}
func (iter *Iter) MapScan(m map[string]interface{}) bool {
	if iter.err != nil {
		return false
	}

	cols := iter.Columns()
	columnNames := make([]string, 0, len(cols))
	values := make([]interface{}, 0, len(cols))
	for _, column := range iter.Columns() {
		if c, ok := column.TypeInfo.(TupleTypeInfo); ok {
			for i := range c.Elems {
				columnName := TupleColumnName(column.Name, i)
				if dest, ok := m[columnName]; ok {
					values = append(values, dest)
				} else {
					zero := c.Elems[i].Zero()
					// technically this is a *interface{} but later we will fix it
					values = append(values, &zero)
				}
				columnNames = append(columnNames, columnName)
			}
		} else {
			if dest, ok := m[column.Name]; ok {
				values = append(values, dest)
			} else {
				zero := column.TypeInfo.Zero()
				// technically this is a *interface{} but later we will fix it
				values = append(values, &zero)
			}
			columnNames = append(columnNames, column.Name)
		}
	}
	if iter.Scan(values...) {
		for i, name := range columnNames {
			if iptr, ok := values[i].(*interface{}); ok {
				m[name] = *iptr
			} else {
				// TODO: it seems wrong to dereference the values that were passed in
				// originally in the map but that's what it was doing before
				m[name] = dereference(values[i])
			}
		}
		return true
	}
	return false
}

func copyBytes(p []byte) []byte {
	b := make([]byte, len(p))
	copy(b, p)
	return b
}

var failDNS = false

func LookupIP(host string) ([]net.IP, error) {
	if failDNS {
		return nil, &net.DNSError{}
	}
	return net.LookupIP(host)

}

func ringString(hosts []*HostInfo) string {
	buf := new(bytes.Buffer)
	for _, h := range hosts {
		buf.WriteString("[" + h.ConnectAddress().String() + "-" + h.HostID() + ":" + h.State().String() + "]")
	}
	return buf.String()
}
