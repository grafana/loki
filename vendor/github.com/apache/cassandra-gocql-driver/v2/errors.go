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
 * Copyright (c) 2016, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

import "fmt"

// See CQL Binary Protocol v5, section 8 for more details.
// https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec
const (
	// ErrCodeServer indicates unexpected error on server-side.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1246-L1247
	ErrCodeServer = 0x0000
	// ErrCodeProtocol indicates a protocol violation by some client message.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1248-L1250
	ErrCodeProtocol = 0x000A
	// ErrCodeCredentials indicates missing required authentication.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1251-L1254
	ErrCodeCredentials = 0x0100
	// ErrCodeUnavailable indicates unavailable error.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1255-L1265
	ErrCodeUnavailable = 0x1000
	// ErrCodeOverloaded returned in case of request on overloaded node coordinator.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1266-L1267
	ErrCodeOverloaded = 0x1001
	// ErrCodeBootstrapping returned from the coordinator node in bootstrapping phase.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1268-L1269
	ErrCodeBootstrapping = 0x1002
	// ErrCodeTruncate indicates truncation exception.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1270
	ErrCodeTruncate = 0x1003
	// ErrCodeWriteTimeout returned in case of timeout during the request write.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1271-L1304
	ErrCodeWriteTimeout = 0x1100
	// ErrCodeReadTimeout returned in case of timeout during the request read.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1305-L1321
	ErrCodeReadTimeout = 0x1200
	// ErrCodeReadFailure indicates request read error which is not covered by ErrCodeReadTimeout.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1322-L1340
	ErrCodeReadFailure = 0x1300
	// ErrCodeFunctionFailure indicates an error in user-defined function.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1341-L1347
	ErrCodeFunctionFailure = 0x1400
	// ErrCodeWriteFailure indicates request write error which is not covered by ErrCodeWriteTimeout.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1348-L1385
	ErrCodeWriteFailure = 0x1500
	// ErrCodeCDCWriteFailure is defined, but not yet documented in CQLv5 protocol.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1386
	ErrCodeCDCWriteFailure = 0x1600
	// ErrCodeCASWriteUnknown indicates only partially completed CAS operation.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1387-L1397
	ErrCodeCASWriteUnknown = 0x1700
	// ErrCodeSyntax indicates the syntax error in the query.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1399
	ErrCodeSyntax = 0x2000
	// ErrCodeUnauthorized indicates access rights violation by user on performed operation.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1400-L1401
	ErrCodeUnauthorized = 0x2100
	// ErrCodeInvalid indicates invalid query error which is not covered by ErrCodeSyntax.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1402
	ErrCodeInvalid = 0x2200
	// ErrCodeConfig indicates the configuration error.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1403
	ErrCodeConfig = 0x2300
	// ErrCodeAlreadyExists is returned for the requests creating the existing keyspace/table.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1404-L1413
	ErrCodeAlreadyExists = 0x2400
	// ErrCodeUnprepared returned from the host for prepared statement which is unknown.
	//
	// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1414-L1417
	ErrCodeUnprepared = 0x2500
)

// RequestError represents errors returned by Cassandra server.
type RequestError interface {
	Code() int
	Message() string
	Error() string
}

type errorFrame struct {
	frameHeader

	code    int
	message string
}

func (e errorFrame) Code() int {
	return e.code
}

func (e errorFrame) Message() string {
	return e.message
}

func (e errorFrame) Error() string {
	return e.Message()
}

func (e errorFrame) String() string {
	return fmt.Sprintf("[error code=%x message=%q]", e.code, e.message)
}

// RequestErrUnavailable represents an unavailable error returned by Cassandra.
// This error occurs when there are not enough nodes available to fulfill the request.
type RequestErrUnavailable struct {
	errorFrame
	Consistency Consistency
	Required    int
	Alive       int
}

func (e *RequestErrUnavailable) String() string {
	return fmt.Sprintf("[request_error_unavailable consistency=%s required=%d alive=%d]", e.Consistency, e.Required, e.Alive)
}

// ErrorMap maps node IP addresses to their respective error codes for read/write failure responses.
// Each entry represents a node that failed during the operation, with the key being the node's
// IP address as a string and the value being the specific error code returned by that node.
type ErrorMap map[string]uint16

// RequestErrWriteTimeout represents a write timeout error returned by Cassandra.
// This error occurs when a write request times out after the coordinator
// has successfully written to some replicas but not enough to satisfy the required consistency level.
type RequestErrWriteTimeout struct {
	errorFrame
	Consistency Consistency
	Received    int
	BlockFor    int
	WriteType   string
}

// RequestErrWriteFailure represents a write failure error returned by Cassandra.
// This error occurs when a write request fails on one or more replicas.
type RequestErrWriteFailure struct {
	errorFrame
	Consistency Consistency
	Received    int
	BlockFor    int
	NumFailures int
	WriteType   string
	ErrorMap    ErrorMap
}

// RequestErrCDCWriteFailure represents a CDC write failure error returned by Cassandra.
// This error occurs when a write to the Change Data Capture log fails.
type RequestErrCDCWriteFailure struct {
	errorFrame
}

// RequestErrReadTimeout represents a read timeout error returned by Cassandra.
// This error occurs when a read request times out after the coordinator
// has received some responses but not enough to satisfy the required consistency level.
type RequestErrReadTimeout struct {
	errorFrame
	Consistency Consistency
	Received    int
	BlockFor    int
	DataPresent byte
}

// RequestErrAlreadyExists represents an "already exists" error returned by Cassandra.
// This error occurs when attempting to create a keyspace or table that already exists.
type RequestErrAlreadyExists struct {
	errorFrame
	Keyspace string
	Table    string
}

// RequestErrUnprepared represents an "unprepared" error returned by Cassandra.
// This error occurs when a prepared statement is no longer available on the server.
type RequestErrUnprepared struct {
	errorFrame
	StatementId []byte
}

// RequestErrReadFailure represents a read failure error returned by Cassandra.
// This error occurs when a read request fails on one or more replicas.
type RequestErrReadFailure struct {
	errorFrame
	Consistency Consistency
	Received    int
	BlockFor    int
	NumFailures int
	DataPresent bool
	ErrorMap    ErrorMap
}

// RequestErrFunctionFailure represents a function failure error returned by Cassandra.
// This error occurs when a user-defined function fails during execution.
type RequestErrFunctionFailure struct {
	errorFrame
	Keyspace string
	Function string
	ArgTypes []string
}

// RequestErrCASWriteUnknown is distinct error for ErrCodeCasWriteUnknown.
//
// See https://github.com/apache/cassandra/blob/7337fc0/doc/native_protocol_v5.spec#L1387-L1397
type RequestErrCASWriteUnknown struct {
	errorFrame
	Consistency Consistency
	Received    int
	BlockFor    int
}
