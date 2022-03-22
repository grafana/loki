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

package errors

// Error severity codes
const (
	Eunknown int8 = iota
	Efatal
	Eerror
	Ewarning
)

// RPCMetadata contains metadata about the call that caused the error.
type RPCMetadata struct {
	ServerAddress string
}

// ErrorCode represents the error code returned by the avatica server
type ErrorCode uint32

// SQLState represents the SQL code returned by the avatica server
type SQLState string

// ResponseError is an error type that contains detailed information on
// what caused the server to return an error.
type ResponseError struct {
	Exceptions    []string
	HasExceptions bool
	ErrorMessage  string
	Severity      int8
	ErrorCode     ErrorCode
	SqlState      SQLState
	Metadata      *RPCMetadata
	Name          string
}

func (r ResponseError) Error() string {

	msg := "An error was encountered while processing your request"

	if r.ErrorMessage != "" {
		msg += ": " + r.ErrorMessage
	} else if len(r.Exceptions) > 0 {
		msg += ":\n" + r.Exceptions[0]
	}

	return msg
}
