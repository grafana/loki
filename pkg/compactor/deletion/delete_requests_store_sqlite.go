package deletion

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

const (
	columnNameID              = "id"
	columnNameUserID          = "user_id"
	columnNameCreatedAt       = "created_at"
	columnNameStartTime       = "start_time"
	columnNameEndTime         = "end_time"
	columnNameTotalShards     = "total_shards"
	columnNameProcessedShards = "processed_shards"
	columnNameQuery           = "query"
	columnNameGenNum          = "gen_num"
)

const (
	sqlCreateDeleteRequestsTable = `CREATE TABLE IF NOT EXISTS requests (
       id TEXT PRIMARY KEY,
       user_id TEXT NOT NULL,
       created_at INT NOT NULL,
       completed_at INT,
       start_time INT NOT NULL,
       end_time INT NOT NULL,
       total_shards INT NOT NULL,
       processed_shards INT DEFAULT 0,
       query TEXT NOT NULL
    );`
	sqlCreateRequestsTableIndex                = `CREATE INDEX IF NOT EXISTS idx_requests_user_id ON requests(user_id);`
	sqlCreateRequestsTableUserCompletedAtIndex = `CREATE INDEX IF NOT EXISTS idx_requests_user_completed ON requests(user_id, completed_at);`
	sqlCreateDeleteRequestShardsTable          = `CREATE TABLE IF NOT EXISTS shards (
       id TEXT NOT NULL,
       user_id TEXT NOT NULL,
       start_time INT NOT NULL,
       end_time INT NOT NULL,
       FOREIGN KEY (id) REFERENCES requests(id)
    );`
	sqlCreateShardsTableIndex = `CREATE INDEX IF NOT EXISTS idx_shards_id_user ON shards(id, user_id);`
	sqlCreateCacheGenTable    = `CREATE TABLE IF NOT EXISTS cache_gen (
        user_id TEXT PRIMARY KEY,
        gen_num INT NOT NULL
    );`
	sqlCreateCacheTableIndex = `CREATE INDEX IF NOT EXISTS idx_cache_gen_user_id ON shards(user_id);`

	sqlInsertDeleteRequest = `INSERT INTO requests (id, user_id, created_at, start_time, end_time, total_shards, query) 
                             VALUES (?, ?, ?, ?, ?, ?, ?);`
	sqlInsertDeleteRequestShard = `INSERT INTO shards VALUES (?, ?, ?, ?)`
	sqlUpdateCacheGen           = `INSERT OR REPLACE INTO cache_gen VALUES (?, ?);`
	sqlDeleteShard              = `DELETE FROM shards WHERE id=? AND start_time=? AND end_time=?;`
	sqlProcessedShardUpdate     = `UPDATE requests
                              SET
                                 processed_shards=processed_shards+1,
                                 completed_at = CASE
                                     WHEN processed_shards+1 = total_shards THEN ?
                                    ELSE NULL
                                 END
                              WHERE id=?`
	sqlDeleteShards          = `DELETE FROM shards WHERE id=? AND user_id=?;`
	sqlRemoveDeleteRequest   = `DELETE FROM requests WHERE id=? AND user_id=?`
	sqlSelectRequestByID     = `SELECT * FROM requests WHERE id = ? AND user_id = ?;`
	sqlSelectRequests        = `SELECT * FROM requests;`
	sqlSelectRequestsForUser = `SELECT * FROM requests WHERE user_id = ?;`
	// while listing requests for query-time filtering, consider only the requests which are unprocessed or
	// a specific duration has elapsed since they completed, to let the index updates get propagated.
	sqlSelectUserRequestsForQueryTimeFiltering = `SELECT * FROM requests WHERE user_id = ? AND (completed_at IS NULL OR completed_at > ?);`
	sqlSelectCacheGen                          = `SELECT gen_num FROM cache_gen WHERE user_id = ?;`
	sqlGetUnprocessedShards                    = `SELECT dr.id, dr.user_id, dr.created_at, sh.start_time, sh.end_time, dr.query
                              FROM shards sh
                              JOIN requests dr ON sh.id = dr.id`
	sqlCountDeleteRequests = `SELECT COUNT(*) FROM requests;`
)

type userCacheGen struct {
	userID, cacheGen string
}

// deleteRequestsStoreSQLite provides all the methods required to manage lifecycle of delete request and things related to it.
type deleteRequestsStoreSQLite struct {
	sqliteStore                    *sqliteDB
	indexUpdatePropagationMaxDelay time.Duration
}

func newDeleteRequestsStoreSQLite(workingDirectory string, indexStorageClient storage.Client, indexUpdatePropagationMaxDelay time.Duration) (*deleteRequestsStoreSQLite, error) {
	sqliteStore, err := newSQLiteDB(workingDirectory, indexStorageClient)
	if err != nil {
		return nil, err
	}
	err = sqliteStore.Exec(
		context.Background(),
		true,
		sqlQuery{query: sqlCreateDeleteRequestsTable},
		sqlQuery{query: sqlCreateRequestsTableIndex},
		sqlQuery{query: sqlCreateRequestsTableUserCompletedAtIndex},
		sqlQuery{query: sqlCreateDeleteRequestShardsTable},
		sqlQuery{query: sqlCreateShardsTableIndex},
		sqlQuery{query: sqlCreateCacheGenTable},
		sqlQuery{query: sqlCreateCacheTableIndex},
	)
	if err != nil {
		return nil, err
	}

	s := &deleteRequestsStoreSQLite{
		sqliteStore:                    sqliteStore,
		indexUpdatePropagationMaxDelay: indexUpdatePropagationMaxDelay,
	}

	return s, nil
}

func (ds *deleteRequestsStoreSQLite) Stop() {
	ds.sqliteStore.Stop()
}

func (ds *deleteRequestsStoreSQLite) copyData(ctx context.Context, shards []DeleteRequest, userCacheGens []userCacheGen) error {
	slices.SortFunc(shards, func(a, b DeleteRequest) int {
		return strings.Compare(a.RequestID, b.RequestID)
	})
	mergedReqs := mergeDeletes(shards)

	var sqlQueries []sqlQuery

	for _, req := range mergedReqs {
		var idxStart, idxEnd int
		for i := range shards {
			if req.RequestID == shards[i].RequestID {
				idxStart = i
				break
			}
		}

		for i := len(shards) - 1; i > 0; i-- {
			if req.RequestID == shards[i].RequestID {
				idxEnd = i
				break
			}
		}

		sqlQueries = append(sqlQueries, ds.buildAddDeleteRequestQueries(req, shards[idxStart:idxEnd+1])...)
	}

	for _, shard := range shards {
		if shard.Status != StatusProcessed {
			continue
		}

		sqlQueries = append(sqlQueries, ds.buildMarkShardAsProcessedQueries(shard)...)
	}

	for _, userCacheGen := range userCacheGens {
		sqlQueries = append(sqlQueries, sqlQuery{
			query: sqlUpdateCacheGen,
			execOpts: &sqlitex.ExecOptions{
				Args: []any{
					userCacheGen.userID,
					userCacheGen.cacheGen,
				},
			},
		})
	}

	return ds.sqliteStore.Exec(ctx, true, sqlQueries...)
}

func (ds *deleteRequestsStoreSQLite) buildAddDeleteRequestQueries(req DeleteRequest, shards []DeleteRequest) []sqlQuery {
	sqlQueries := []sqlQuery{
		{
			query: sqlInsertDeleteRequest,
			execOpts: &sqlitex.ExecOptions{
				Args: []any{
					req.RequestID,
					req.UserID,
					req.CreatedAt,
					req.StartTime,
					req.EndTime,
					len(shards),
					req.Query,
				},
			},
		},
	}

	for _, shard := range shards {
		sqlQueries = append(sqlQueries, sqlQuery{
			query: sqlInsertDeleteRequestShard,
			execOpts: &sqlitex.ExecOptions{
				Args: []any{
					req.RequestID,
					req.UserID,
					shard.StartTime,
					shard.EndTime,
				},
			},
		})
	}

	return sqlQueries
}

func (ds *deleteRequestsStoreSQLite) isEmpty(ctx context.Context) (bool, error) {
	isEmpty := true
	err := ds.sqliteStore.Exec(ctx, false, sqlQuery{
		query: sqlCountDeleteRequests,
		execOpts: &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				if stmt.ColumnInt(0) != 0 {
					isEmpty = false
				}
				return nil
			},
		},
	})
	if err != nil {
		return false, err
	}

	return isEmpty, err
}

// AddDeleteRequest creates entries for new delete requests. All passed delete requests will be associated to
// each other by request id
func (ds *deleteRequestsStoreSQLite) AddDeleteRequest(ctx context.Context, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) (string, error) {
	// Generate unique request ID
	requestID := generateUniqueID(userID, query)

	// Use common implementation
	err := ds.addDeleteRequestWithID(ctx, requestID, userID, query, startTime, endTime, shardByInterval)
	if err != nil {
		return "", err
	}

	return requestID, nil
}

func (ds *deleteRequestsStoreSQLite) addDeleteRequestWithID(ctx context.Context, requestID, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) error {
	var req DeleteRequest

	req.RequestID = requestID
	req.UserID = userID
	req.Query = query
	req.StartTime = startTime
	req.EndTime = endTime
	req.CreatedAt = model.Now()
	shards := buildRequests(shardByInterval, query, userID, startTime, endTime)
	if len(shards) == 0 {
		return fmt.Errorf("zero delete requests created")
	}

	sqlQueries := ds.buildAddDeleteRequestQueries(req, shards)
	sqlQueries = append(sqlQueries, sqlQuery{
		query: sqlUpdateCacheGen,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{
				req.UserID,
				time.Now().UnixNano(),
			},
		},
	})

	return ds.sqliteStore.Exec(ctx, true, sqlQueries...)
}

func (ds *deleteRequestsStoreSQLite) generateID(ctx context.Context, req DeleteRequest) (string, error) {
	requestID := generateUniqueID(req.UserID, req.Query)

	for {
		count := 0
		if err := ds.sqliteStore.Exec(ctx, false, sqlQuery{
			query: sqlSelectRequestByID,
			execOpts: &sqlitex.ExecOptions{
				Args: []any{
					requestID,
					req.UserID,
				},
				ResultFunc: func(_ *sqlite.Stmt) error {
					count++
					return nil
				},
			},
		}); err != nil {
			return "", err
		}
		if count == 0 {
			return requestID, nil
		}

		// we have a collision here, lets recreate a new requestID and check for collision
		time.Sleep(time.Millisecond)
		requestID = generateUniqueID(req.UserID, req.Query)
	}
}

func (ds *deleteRequestsStoreSQLite) RemoveDeleteRequest(ctx context.Context, userID string, requestID string) error {
	return ds.sqliteStore.Exec(ctx, true, sqlQuery{
		query: sqlDeleteShards,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{
				requestID,
				userID,
			},
		},
	}, sqlQuery{
		query: sqlRemoveDeleteRequest,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{
				requestID,
				userID,
			},
		},
	}, sqlQuery{
		query: sqlUpdateCacheGen,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{
				userID,
				time.Now().UnixNano(),
			},
		},
	})
}

func (ds *deleteRequestsStoreSQLite) GetDeleteRequest(ctx context.Context, userID, requestID string) (DeleteRequest, error) {
	reqs, err := ds.queryDeleteRequests(ctx, sqlSelectRequestByID, []any{
		requestID,
		userID,
	})
	if len(reqs) == 0 {
		return DeleteRequest{}, ErrDeleteRequestNotFound
	}

	return reqs[0], err
}

func (ds *deleteRequestsStoreSQLite) MergeShardedRequests(_ context.Context) error {
	return nil
}

func (ds *deleteRequestsStoreSQLite) MarkShardAsProcessed(ctx context.Context, req DeleteRequest) error {
	return ds.sqliteStore.Exec(ctx, true, ds.buildMarkShardAsProcessedQueries(req)...)
}

func (ds *deleteRequestsStoreSQLite) buildMarkShardAsProcessedQueries(req DeleteRequest) []sqlQuery {
	return []sqlQuery{
		{
			query: sqlDeleteShard,
			execOpts: &sqlitex.ExecOptions{
				Args: []any{
					req.RequestID,
					req.StartTime,
					req.EndTime,
				},
			},
		}, {
			query: sqlProcessedShardUpdate,
			execOpts: &sqlitex.ExecOptions{
				Args: []any{
					model.Now(),
					req.RequestID,
				},
			},
		},
	}
}

func (ds *deleteRequestsStoreSQLite) GetUnprocessedShards(ctx context.Context) ([]DeleteRequest, error) {
	var requests []DeleteRequest
	if err := ds.sqliteStore.Exec(ctx, false, sqlQuery{
		query: sqlGetUnprocessedShards,
		execOpts: &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				requests = append(requests, DeleteRequest{
					RequestID: stmt.GetText(columnNameID),
					UserID:    stmt.GetText(columnNameUserID),
					CreatedAt: model.Time(stmt.GetInt64(columnNameCreatedAt)),
					StartTime: model.Time(stmt.GetInt64(columnNameStartTime)),
					EndTime:   model.Time(stmt.GetInt64(columnNameEndTime)),
					Query:     stmt.GetText(columnNameQuery),
					Status:    StatusReceived,
				})
				return nil
			},
		},
	}); err != nil {
		return nil, err
	}

	return requests, nil
}

func (ds *deleteRequestsStoreSQLite) GetAllRequests(ctx context.Context) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, sqlSelectRequests, nil)
}

// GetAllDeleteRequestsForUser returns all delete requests for a user.
func (ds *deleteRequestsStoreSQLite) GetAllDeleteRequestsForUser(ctx context.Context, userID string, forQuerytimeFiltering bool) ([]DeleteRequest, error) {
	if !forQuerytimeFiltering {
		return ds.queryDeleteRequests(ctx, sqlSelectRequestsForUser, []any{userID})
	}
	// for time elapsed since the requests got processed, consider the given index update propagation delay
	return ds.queryDeleteRequests(ctx, sqlSelectUserRequestsForQueryTimeFiltering, []any{userID, model.Now().Add(-ds.indexUpdatePropagationMaxDelay)})
}

func (ds *deleteRequestsStoreSQLite) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	genNumber := ""
	err := ds.sqliteStore.Exec(ctx, false, sqlQuery{
		query: sqlSelectCacheGen,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{
				userID,
			},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				genNumber = stmt.GetText(columnNameGenNum)
				return nil
			},
		},
	})
	if err != nil {
		return "", err
	}

	return genNumber, nil
}

func (ds *deleteRequestsStoreSQLite) queryDeleteRequests(ctx context.Context, query string, args []any) ([]DeleteRequest, error) {
	var requests []DeleteRequest
	if err := ds.sqliteStore.Exec(ctx, false, sqlQuery{
		query: query,
		execOpts: &sqlitex.ExecOptions{
			Args: args,
			ResultFunc: func(stmt *sqlite.Stmt) error {
				requests = append(requests, DeleteRequest{
					RequestID: stmt.GetText(columnNameID),
					UserID:    stmt.GetText(columnNameUserID),
					CreatedAt: model.Time(stmt.GetInt64(columnNameCreatedAt)),
					StartTime: model.Time(stmt.GetInt64(columnNameStartTime)),
					EndTime:   model.Time(stmt.GetInt64(columnNameEndTime)),
					Query:     stmt.GetText(columnNameQuery),
					Status:    deleteRequestStatus(int(stmt.GetInt64(columnNameProcessedShards)), int(stmt.GetInt64(columnNameTotalShards))),
				})
				return nil
			},
		},
	}); err != nil {
		return nil, err
	}

	return requests, nil
}
