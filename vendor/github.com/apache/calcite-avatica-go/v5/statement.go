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
	"context"
	"database/sql/driver"
	"math"
	"sync"
	"time"

	"github.com/apache/calcite-avatica-go/v5/message"
	"golang.org/x/xerrors"
)

type stmt struct {
	statementID  uint32
	conn         *conn
	parameters   []*message.AvaticaParameter
	handle       message.StatementHandle
	batchUpdates []*message.UpdateBatch
	sync.Mutex
}

// Close closes a statement
func (s *stmt) Close() error {

	if s.conn.connectionId == "" {
		return driver.ErrBadConn
	}

	if s.conn.config.batching {
		_, err := s.conn.httpClient.post(context.Background(), &message.ExecuteBatchRequest{
			ConnectionId: s.conn.connectionId,
			StatementId:  s.statementID,
			Updates:      s.batchUpdates,
		})
		if err != nil {
			return s.conn.avaticaErrorToResponseErrorOrError(err)
		}
	}

	_, err := s.conn.httpClient.post(context.Background(), &message.CloseStatementRequest{
		ConnectionId: s.conn.connectionId,
		StatementId:  s.statementID,
	})

	if err != nil {
		return s.conn.avaticaErrorToResponseErrorOrError(err)
	}

	return nil
}

// NumInput returns the number of placeholder parameters.
//
// If NumInput returns >= 0, the sql package will sanity check
// argument counts from callers and return errors to the caller
// before the statement's Exec or Query methods are called.
//
// NumInput may also return -1, if the driver doesn't know
// its number of placeholders. In that case, the sql package
// will not sanity check Exec or Query argument counts.
func (s *stmt) NumInput() int {
	return len(s.parameters)
}

// Exec executes a query that doesn't return rows, such
// as an INSERT or UPDATE.
func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	list := driverValueToNamedValue(args)
	return s.exec(context.Background(), list)
}

func (s *stmt) exec(ctx context.Context, args []namedValue) (driver.Result, error) {

	if s.conn.connectionId == "" {
		return nil, driver.ErrBadConn
	}

	values := s.parametersToTypedValues(args)

	if s.conn.config.batching {
		s.Lock()
		defer s.Unlock()

		s.batchUpdates = append(s.batchUpdates, &message.UpdateBatch{
			ParameterValues: values,
		})
		return &result{
			affectedRows: -1,
		}, nil
	}

	msg := &message.ExecuteRequest{
		StatementHandle:    &s.handle,
		ParameterValues:    values,
		FirstFrameMaxSize:  s.conn.config.frameMaxSize,
		HasParameterValues: true,
	}

	if s.conn.config.frameMaxSize <= -1 {
		msg.DeprecatedFirstFrameMaxSize = math.MaxInt64
	} else {
		msg.DeprecatedFirstFrameMaxSize = uint64(s.conn.config.frameMaxSize)
	}

	res, err := s.conn.httpClient.post(ctx, msg)

	if err != nil {
		return nil, s.conn.avaticaErrorToResponseErrorOrError(err)
	}

	results := res.(*message.ExecuteResponse).Results

	if len(results) <= 0 {
		return nil, xerrors.New("empty ResultSet in ExecuteResponse")
	}

	// Currently there is only 1 ResultSet per response
	changed := int64(results[0].UpdateCount)

	return &result{
		affectedRows: changed,
	}, nil
}

// Query executes a query that may return rows, such as a
// SELECT.
func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	list := driverValueToNamedValue(args)
	return s.query(context.Background(), list)
}

func (s *stmt) query(ctx context.Context, args []namedValue) (driver.Rows, error) {
	if s.conn.connectionId == "" {
		return nil, driver.ErrBadConn
	}

	msg := &message.ExecuteRequest{
		StatementHandle:    &s.handle,
		ParameterValues:    s.parametersToTypedValues(args),
		FirstFrameMaxSize:  s.conn.config.frameMaxSize,
		HasParameterValues: true,
	}

	if s.conn.config.frameMaxSize <= -1 {
		msg.DeprecatedFirstFrameMaxSize = math.MaxInt64
	} else {
		msg.DeprecatedFirstFrameMaxSize = uint64(s.conn.config.frameMaxSize)
	}

	res, err := s.conn.httpClient.post(ctx, msg)

	if err != nil {
		return nil, s.conn.avaticaErrorToResponseErrorOrError(err)
	}

	resultSet := res.(*message.ExecuteResponse).Results

	return newRows(s.conn, s.statementID, resultSet), nil
}

func (s *stmt) parametersToTypedValues(vals []namedValue) []*message.TypedValue {

	var result []*message.TypedValue

	for i, val := range vals {
		typed := message.TypedValue{}
		if val.Value == nil {
			typed.Null = true
			typed.Type = message.Rep_NULL
		} else {

			switch v := val.Value.(type) {
			case int64:
				typed.Type = message.Rep_LONG
				typed.NumberValue = v
			case float64:
				typed.Type = message.Rep_DOUBLE
				typed.DoubleValue = v
			case bool:
				typed.Type = message.Rep_BOOLEAN
				typed.BoolValue = v
			case []byte:
				typed.Type = message.Rep_BYTE_STRING
				typed.BytesValue = v
			case string:

				if s.parameters[i].TypeName == "DECIMAL" {
					typed.Type = message.Rep_BIG_DECIMAL
				} else {
					typed.Type = message.Rep_STRING
				}
				typed.StringValue = v

			case time.Time:
				avaticaParameter := s.parameters[i]

				switch avaticaParameter.TypeName {
				case "TIME", "UNSIGNED_TIME":
					typed.Type = message.Rep_JAVA_SQL_TIME

					// Because a location can have multiple time zones due to daylight savings,
					// we need to be explicit and get the offset
					zone, offset := v.Zone()

					// Calculate milliseconds since 00:00:00.000
					base := time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.FixedZone(zone, offset))
					typed.NumberValue = int64(v.Sub(base).Nanoseconds() / int64(time.Millisecond))

				case "DATE", "UNSIGNED_DATE":
					typed.Type = message.Rep_JAVA_SQL_DATE

					// Because a location can have multiple time zones due to daylight savings,
					// we need to be explicit and get the offset
					zone, offset := v.Zone()

					// Calculate number of days since 1970/1/1
					base := time.Date(1970, 1, 1, 0, 0, 0, 0, time.FixedZone(zone, offset))
					typed.NumberValue = int64(v.Sub(base) / (24 * time.Hour))

				case "TIMESTAMP", "UNSIGNED_TIMESTAMP":
					typed.Type = message.Rep_JAVA_SQL_TIMESTAMP

					// Because a location can have multiple time zones due to daylight savings,
					// we need to be explicit and get the offset
					zone, offset := v.Zone()

					// Calculate number of milliseconds since 1970-01-01 00:00:00.000
					base := time.Date(1970, 1, 1, 0, 0, 0, 0, time.FixedZone(zone, offset))
					typed.NumberValue = int64(v.Sub(base).Nanoseconds() / int64(time.Millisecond))
				}
			}
		}

		result = append(result, &typed)
	}

	return result
}
