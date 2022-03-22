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
	"context"
	"database/sql"
	"database/sql/driver"

	"golang.org/x/xerrors"
)

func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {

	if opts.ReadOnly {
		return nil, xerrors.New("read-only transactions are not supported")
	}

	var isolation isoLevel

	switch sql.IsolationLevel(opts.Isolation) {
	case sql.LevelDefault:
		isolation = isolationUseCurrent
	case sql.LevelReadUncommitted:
		isolation = isolationReadUncommitted
	case sql.LevelReadCommitted:
		isolation = isolationReadComitted
	case sql.LevelWriteCommitted:
		return nil, xerrors.New("LevelWriteCommitted isolation level is not supported")
	case sql.LevelRepeatableRead:
		isolation = isolationRepeatableRead
	case sql.LevelSnapshot:
		return nil, xerrors.New("LevelSnapshot isolation level is not supported")
	case sql.LevelSerializable:
		isolation = isolationSerializable
	case sql.LevelLinearizable:
		return nil, xerrors.New("LevelLinearizable isolation level is not supported")
	default:
		return nil, xerrors.Errorf("unsupported transaction isolation level: %d", opts.Isolation)
	}

	return c.begin(ctx, isolation)
}

func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.prepare(ctx, query)
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	list, err := driverNamedValueToNamedValue(args)

	if err != nil {
		return nil, xerrors.Errorf("could not execute statement: %v", err)
	}

	return c.exec(ctx, query, list)
}

func (c *conn) Ping(ctx context.Context) error {

	_, err := c.ExecContext(ctx, c.adapter.GetPingStatement(), []driver.NamedValue{})

	if err != nil {
		return xerrors.Errorf("error pinging database: %v", err)
	}

	return nil
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	list, err := driverNamedValueToNamedValue(args)

	if err != nil {
		return nil, xerrors.Errorf("could not execute query: %v", err)
	}

	return c.query(ctx, query, list)
}
