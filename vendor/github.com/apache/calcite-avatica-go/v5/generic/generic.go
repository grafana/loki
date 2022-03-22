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

package generic

import (
	"math"
	"reflect"
	"time"

	"github.com/apache/calcite-avatica-go/v5/errors"
	"github.com/apache/calcite-avatica-go/v5/internal"
	"github.com/apache/calcite-avatica-go/v5/message"
)

type Adapter struct {
}

func (a Adapter) GetPingStatement() string {
	return "SELECT 1"
}

func (a Adapter) GetColumnTypeDefinition(col *message.ColumnMetaData) *internal.Column {

	column := &internal.Column{
		Name:     col.ColumnName,
		TypeName: col.Type.Name,
		Nullable: col.Nullable != 0,
	}

	// Handle precision and length
	switch col.Type.Name {
	case "DECIMAL":

		precision := int64(col.Precision)

		if precision == 0 {
			precision = math.MaxInt64
		}

		scale := int64(col.Scale)

		if scale == 0 {
			scale = math.MaxInt64
		}

		column.PrecisionScale = &internal.PrecisionScale{
			Precision: precision,
			Scale:     scale,
		}
	case "VARCHAR", "CHARACTER VARYING", "CHAR", "CHARACTER", "BINARY", "VARBINARY", "BINARY VARYING":
		column.Length = int64(col.Precision)
	}

	// Handle scan types
	switch col.Type.Name {
	case "INTEGER", "BIGINT", "TINYINT", "SMALLINT":
		column.ScanType = reflect.TypeOf(int64(0))

	case "REAL", "FLOAT", "DOUBLE":
		column.ScanType = reflect.TypeOf(float64(0))

	case "DECIMAL", "NUMERIC", "VARCHAR", "CHAR", "CHARACTER", "CHARACTER VARYING":
		column.ScanType = reflect.TypeOf("")

	case "BOOLEAN":
		column.ScanType = reflect.TypeOf(false)

	case "TIME", "DATE", "TIMESTAMP", "TIMESTAMP WITH LOCAL TIME ZONE", "TIMESTAMP WITH TIME ZONE":
		column.ScanType = reflect.TypeOf(time.Time{})

	case "BINARY", "VARBINARY", "BINARY VARYING":
		column.ScanType = reflect.TypeOf([]byte{})

	default:
		column.ScanType = reflect.TypeOf(new(interface{})).Elem()
	}

	// Handle rep type special cases for decimals, floats, date, time and timestamp
	switch col.Type.Name {
	case "DECIMAL", "NUMERIC":
		column.Rep = message.Rep_BIG_DECIMAL
	case "FLOAT", "REAL":
		column.Rep = message.Rep_FLOAT
	case "TIME":
		column.Rep = message.Rep_JAVA_SQL_TIME
	case "DATE":
		column.Rep = message.Rep_JAVA_SQL_DATE
	case "TIMESTAMP", "TIMESTAMP WITH LOCAL TIME ZONE", "TIMESTAMP WITH TIME ZONE":
		column.Rep = message.Rep_JAVA_SQL_TIMESTAMP
	default:
		column.Rep = col.Type.Rep
	}

	return column
}

func (a Adapter) ErrorResponseToResponseError(err *message.ErrorResponse) errors.ResponseError {
	return errors.ResponseError{
		Exceptions:   err.Exceptions,
		ErrorMessage: err.ErrorMessage,
		Severity:     int8(err.Severity),
		ErrorCode:    errors.ErrorCode(err.ErrorCode),
		SqlState:     errors.SQLState(err.SqlState),
		Metadata: &errors.RPCMetadata{
			ServerAddress: message.ServerAddressFromMetadata(err),
		},
	}
}
