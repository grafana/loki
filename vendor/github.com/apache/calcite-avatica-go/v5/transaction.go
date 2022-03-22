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

	"github.com/apache/calcite-avatica-go/v5/message"
)

type tx struct {
	conn *conn
}

// Commit commits a transaction
func (t *tx) Commit() error {

	defer t.enableAutoCommit()

	_, err := t.conn.httpClient.post(context.Background(), &message.CommitRequest{
		ConnectionId: t.conn.connectionId,
	})

	if err != nil {
		return t.conn.avaticaErrorToResponseErrorOrError(err)
	}

	return nil
}

// Rollback rolls back a transaction
func (t *tx) Rollback() error {

	defer t.enableAutoCommit()

	_, err := t.conn.httpClient.post(context.Background(), &message.RollbackRequest{
		ConnectionId: t.conn.connectionId,
	})

	if err != nil {
		return t.conn.avaticaErrorToResponseErrorOrError(err)
	}

	return nil
}

// enableAutoCommit enables auto-commit on the server
func (t *tx) enableAutoCommit() error {

	_, err := t.conn.httpClient.post(context.Background(), &message.ConnectionSyncRequest{
		ConnectionId: t.conn.connectionId,
		ConnProps: &message.ConnectionProperties{
			AutoCommit:           true,
			HasAutoCommit:        true,
			TransactionIsolation: t.conn.config.transactionIsolation,
		},
	})

	if err != nil {
		return t.conn.avaticaErrorToResponseErrorOrError(err)
	}

	return nil
}
