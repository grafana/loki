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
	"strings"

	avaticaMessage "github.com/apache/calcite-avatica-go/v5/message"
	"github.com/golang/protobuf/proto"
	"golang.org/x/xerrors"
)

const (
	wireMessageRequestPrefix  = "org.apache.calcite.avatica.proto.Requests$"
	wireMessageResponsePrefix = "org.apache.calcite.avatica.proto.Responses$"
)

// ClassNameFromRequest takes a message and generates the
// corresponding Java class name.
func classNameFromRequest(message interface{}) string {

	var class string

	switch message.(type) {

	case *avaticaMessage.CatalogsRequest:
		class = "CatalogsRequest"
	case *avaticaMessage.CloseConnectionRequest:
		class = "CloseConnectionRequest"
	case *avaticaMessage.CloseStatementRequest:
		class = "CloseStatementRequest"
	case *avaticaMessage.ColumnsRequest:
		class = "ColumnsRequest"
	case *avaticaMessage.CommitRequest:
		class = "CommitRequest"
	case *avaticaMessage.ConnectionSyncRequest:
		class = "ConnectionSyncRequest"
	case *avaticaMessage.CreateStatementRequest:
		class = "CreateStatementRequest"
	case *avaticaMessage.DatabasePropertyRequest:
		class = "DatabasePropertyRequest"
	case *avaticaMessage.ExecuteRequest:
		class = "ExecuteRequest"
	case *avaticaMessage.ExecuteBatchRequest:
		class = "ExecuteBatchRequest"
	case *avaticaMessage.FetchRequest:
		class = "FetchRequest"
	case *avaticaMessage.OpenConnectionRequest:
		class = "OpenConnectionRequest"
	case *avaticaMessage.PrepareAndExecuteRequest:
		class = "PrepareAndExecuteRequest"
	case *avaticaMessage.PrepareRequest:
		class = "PrepareRequest"
	case *avaticaMessage.SyncResultsRequest:
		class = "SyncResultsRequest"
	case *avaticaMessage.RollbackRequest:
		class = "RollbackRequest"
	case *avaticaMessage.SchemasRequest:
		class = "SchemasRequest"
	case *avaticaMessage.TableTypesRequest:
		class = "TableTypesRequest"
	case *avaticaMessage.TablesRequest:
		class = "TablesRequest"
	case *avaticaMessage.TypeInfoRequest:
		class = "TypeInfoRequest"
	}

	return wireMessageRequestPrefix + class
}

// ResponseFromClassName takes a Java class name and instantiates
// the corresponding message type.
func responseFromClassName(className string) (proto.Message, error) {

	simplifiedClassName := strings.Replace(className, wireMessageResponsePrefix, "", 1)

	switch simplifiedClassName {
	case "CloseConnectionResponse":
		return &avaticaMessage.CloseConnectionResponse{}, nil
	case "CloseStatementResponse":
		return &avaticaMessage.CloseStatementResponse{}, nil
	case "CommitResponse":
		return &avaticaMessage.CommitResponse{}, nil
	case "ConnectionSyncResponse":
		return &avaticaMessage.ConnectionSyncResponse{}, nil
	case "CreateStatementResponse":
		return &avaticaMessage.CreateStatementResponse{}, nil
	case "DatabasePropertyResponse":
		return &avaticaMessage.DatabasePropertyResponse{}, nil
	case "ErrorResponse":
		return &avaticaMessage.ErrorResponse{}, nil
	case "ExecuteResponse":
		return &avaticaMessage.ExecuteResponse{}, nil
	case "ExecuteBatchResponse":
		return &avaticaMessage.ExecuteBatchResponse{}, nil
	case "FetchResponse":
		return &avaticaMessage.FetchResponse{}, nil
	case "OpenConnectionResponse":
		return &avaticaMessage.OpenConnectionResponse{}, nil
	case "PrepareResponse":
		return &avaticaMessage.PrepareResponse{}, nil
	case "ResultSetResponse":
		return &avaticaMessage.ResultSetResponse{}, nil
	case "RollbackResponse":
		return &avaticaMessage.RollbackResponse{}, nil
	case "SyncResultsResponse":
		return &avaticaMessage.SyncResultsResponse{}, nil
	default:
		return nil, xerrors.Errorf("unable to create response from the string: %s", className)
	}
}
