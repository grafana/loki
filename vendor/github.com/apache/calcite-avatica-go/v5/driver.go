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

/*
Package avatica provides an Apache Phoenix Query Server/Avatica driver for Go's database/sql package.

Quickstart

Import the database/sql package along with the avatica driver.

	import "database/sql"
	import _ "github.com/apache/calcite-avatica-go/v5"

	db, err := sql.Open("avatica", "http://phoenix-query-server:8765")

See https://calcite.apache.org/avatica/docs/go_client_reference.html for more details
*/
package avatica

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/http"

	"github.com/apache/calcite-avatica-go/v5/generic"
	"github.com/apache/calcite-avatica-go/v5/hsqldb"
	"github.com/apache/calcite-avatica-go/v5/message"
	"github.com/apache/calcite-avatica-go/v5/phoenix"
	"github.com/hashicorp/go-uuid"
	"golang.org/x/xerrors"
)

// Driver is exported to allow it to be used directly.
type Driver struct{}

// Connector implements the driver.Connector interface
type Connector struct {
	Info   map[string]string
	Client *http.Client

	dsn string
}

// NewConnector creates a new connector
func NewConnector(dsn string) driver.Connector {
	return &Connector{nil, nil, dsn}
}

func (c *Connector) Connect(context.Context) (driver.Conn, error) {

	config, err := ParseDSN(c.dsn)

	if err != nil {
		return nil, xerrors.Errorf("unable to open connection: %v", err)
	}

	httpClient, err := NewHTTPClient(config.endpoint, c.Client, config)

	if err != nil {
		return nil, xerrors.Errorf("unable to create HTTP client: %v", err)
	}

	connectionId, err := uuid.GenerateUUID()
	if err != nil {
		return nil, xerrors.Errorf("error generating connection id: %v", err)
	}

	info := map[string]string{
		"AutoCommit":  "true",
		"Consistency": "8",
	}

	for k, v := range c.Info {
		info[k] = v
	}

	conn := &conn{
		connectionId: connectionId,
		httpClient:   httpClient,
		config:       config,
	}

	// Open a connection to the server
	req := &message.OpenConnectionRequest{
		ConnectionId: connectionId,
		Info:         info,
	}

	if config.schema != "" {
		req.Info["schema"] = config.schema
	}

	_, err = httpClient.post(context.Background(), req)

	if err != nil {
		return nil, conn.avaticaErrorToResponseErrorOrError(err)
	}

	response, err := httpClient.post(context.Background(), &message.DatabasePropertyRequest{
		ConnectionId: connectionId,
	})

	if err != nil {
		return nil, conn.avaticaErrorToResponseErrorOrError(err)
	}

	databasePropertyResponse := response.(*message.DatabasePropertyResponse)

	adapter := ""

	for _, property := range databasePropertyResponse.Props {
		if property.Key.Name == "GET_DRIVER_NAME" {
			adapter = property.Value.StringValue
		}
	}

	conn.adapter = getAdapter(adapter)

	return conn, nil
}

// Driver returns the underlying driver
func (c *Connector) Driver() driver.Driver {
	return &Driver{}
}

// Open a Connection to the server.
// See https://github.com/apache/calcite-avatica-go#dsn for more information
// on how the DSN is formatted.
func (a *Driver) Open(dsn string) (driver.Conn, error) {
	return NewConnector(dsn).Connect(context.TODO())
}

func getAdapter(e string) Adapter {
	switch e {
	case "HSQL Database Engine Driver":
		return hsqldb.Adapter{}
	case "PhoenixEmbeddedDriver":
		return phoenix.Adapter{}
	default:
		return generic.Adapter{}
	}
}

func init() {
	sql.Register("avatica", &Driver{})
}
