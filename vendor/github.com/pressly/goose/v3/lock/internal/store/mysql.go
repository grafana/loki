package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/multierr"
)

// NewMySQL creates a new MySQL-based [LockStore].
func NewMySQL(tableName string) (LockStore, error) {
	if tableName == "" {
		return nil, errors.New("table name must not be empty")
	}
	return &mysqlStore{
		tableName: tableName,
	}, nil
}

var _ LockStore = (*mysqlStore)(nil)

type mysqlStore struct {
	tableName string
}

func (s *mysqlStore) TableExists(
	ctx context.Context,
	db *sql.DB,
) (bool, error) {
	schemaName, tableName := parseTableIdentifier(s.tableName)

	var query string
	var args []any
	if schemaName != "" {
		query = `SELECT EXISTS ( SELECT 1 FROM information_schema.tables WHERE table_schema = ? AND table_name = ? )`
		args = []any{schemaName, tableName}
	} else {
		query = `SELECT EXISTS ( SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ? )`
		args = []any{tableName}
	}

	var exists bool
	if err := db.QueryRowContext(ctx, query, args...).Scan(
		&exists,
	); err != nil {
		return false, fmt.Errorf("table exists: %w", err)
	}
	return exists, nil
}

func (s *mysqlStore) CreateLockTable(
	ctx context.Context,
	db *sql.DB,
) error {
	exists, err := s.TableExists(ctx, db)
	if err != nil {
		return fmt.Errorf("check lock table existence: %w", err)
	}
	if exists {
		return nil
	}

	query := fmt.Sprintf(`CREATE TABLE %s (
		lock_id BIGINT NOT NULL PRIMARY KEY,
		locked BOOLEAN NOT NULL DEFAULT 0,
		locked_at DATETIME(6) NULL,
		locked_by TEXT NULL,
		lease_expires_at DATETIME(6) NULL,
		updated_at DATETIME(6) NULL
	)`, s.tableName)
	if _, err := db.ExecContext(ctx, query); err != nil {
		// Double-check if another process created it concurrently
		if exists, checkErr := s.TableExists(ctx, db); checkErr == nil && exists {
			// Another process created it, that's fine!
			return nil
		}
		return fmt.Errorf("create lock table %q: %w", s.tableName, err)
	}
	return nil
}

func (s *mysqlStore) AcquireLock(
	ctx context.Context,
	db *sql.DB,
	lockID int64,
	lockedBy string,
	leaseDuration time.Duration,
) (_ *AcquireLockResult, retErr error) {
	leaseSeconds := int(leaseDuration.Seconds())

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("acquire lock %d: begin tx: %w", lockID, err)
	}
	defer func() {
		if retErr != nil {
			retErr = multierr.Append(retErr, tx.Rollback())
		}
	}()

	// First, try to insert a brand-new row. If a row already exists for this lock_id,
	// INSERT IGNORE silently no-ops (RowsAffected == 0) and we fall through to the UPDATE.
	insertQ := fmt.Sprintf(`INSERT IGNORE INTO %s (lock_id, locked, locked_at, locked_by, lease_expires_at, updated_at)
		VALUES (?, true, now(6), ?, now(6) + INTERVAL ? SECOND, now(6))`, s.tableName)
	insertRes, err := tx.ExecContext(ctx, insertQ, lockID, lockedBy, leaseSeconds)
	if err != nil {
		return nil, fmt.Errorf("acquire lock %d: %w", lockID, err)
	}
	inserted, err := insertRes.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("acquire lock %d: rows affected: %w", lockID, err)
	}

	if inserted == 0 {
		// Row exists: only steal if currently unlocked or lease has expired.
		updateQ := fmt.Sprintf(`UPDATE %s SET
			locked = true,
			locked_at = now(6),
			locked_by = ?,
			lease_expires_at = now(6) + INTERVAL ? SECOND,
			updated_at = now(6)
		WHERE lock_id = ? AND (locked = false OR lease_expires_at < now(6))`, s.tableName)
		updateRes, err := tx.ExecContext(ctx, updateQ, lockedBy, leaseSeconds, lockID)
		if err != nil {
			return nil, fmt.Errorf("acquire lock %d: %w", lockID, err)
		}
		affected, err := updateRes.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("acquire lock %d: rows affected: %w", lockID, err)
		}
		if affected == 0 {
			// TODO(mf): should we return a special error type here?
			return nil, fmt.Errorf("acquire lock %d: already held by another instance", lockID)
		}
	}

	// MySQL has no RETURNING clause; read the row back to learn the resulting lease.
	selectQ := fmt.Sprintf(`SELECT locked_by, lease_expires_at FROM %s WHERE lock_id = ?`, s.tableName)
	var returnedLockedBy string
	var leaseExpiresAt time.Time
	if err := tx.QueryRowContext(ctx, selectQ, lockID).Scan(
		&returnedLockedBy,
		&leaseExpiresAt,
	); err != nil {
		return nil, fmt.Errorf("acquire lock %d: %w", lockID, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("acquire lock %d: commit: %w", lockID, err)
	}

	// Verify we got the lock by checking the returned locked_by matches our instance ID
	if returnedLockedBy != lockedBy {
		return nil, fmt.Errorf("acquire lock %d: acquired by %s instead of %s", lockID, returnedLockedBy, lockedBy)
	}

	return &AcquireLockResult{
		LockedBy:       returnedLockedBy,
		LeaseExpiresAt: leaseExpiresAt,
	}, nil
}

func (s *mysqlStore) ReleaseLock(
	ctx context.Context,
	db *sql.DB,
	lockID int64,
	lockedBy string,
) (_ *ReleaseLockResult, retErr error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("release lock %d: begin tx: %w", lockID, err)
	}
	defer func() {
		if retErr != nil {
			retErr = multierr.Append(retErr, tx.Rollback())
		}
	}()

	// Release lock only if it's held by the current instance
	updateQ := fmt.Sprintf(`UPDATE %s SET
		locked = false,
		locked_at = NULL,
		locked_by = NULL,
		lease_expires_at = NULL,
		updated_at = now(6)
	WHERE lock_id = ? AND locked_by = ?`, s.tableName)
	res, err := tx.ExecContext(ctx, updateQ, lockID, lockedBy)
	if err != nil {
		return nil, fmt.Errorf("release lock %d: %w", lockID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("release lock %d: rows affected: %w", lockID, err)
	}
	if affected == 0 {
		// TODO(mf): should we return a special error type here?
		return nil, fmt.Errorf("release lock %d: not held by this instance", lockID)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("release lock %d: commit: %w", lockID, err)
	}

	return &ReleaseLockResult{
		LockID: lockID,
	}, nil
}

func (s *mysqlStore) UpdateLease(
	ctx context.Context,
	db *sql.DB,
	lockID int64,
	lockedBy string,
	leaseDuration time.Duration,
) (_ *UpdateLeaseResult, retErr error) {
	leaseSeconds := int(leaseDuration.Seconds())

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to update lease for lock %d: begin tx: %w", lockID, err)
	}
	defer func() {
		if retErr != nil {
			retErr = multierr.Append(retErr, tx.Rollback())
		}
	}()

	// Update lease expiration time for heartbeat, only if we own the lock
	updateQ := fmt.Sprintf(`UPDATE %s SET
		lease_expires_at = now(6) + INTERVAL ? SECOND,
		updated_at = now(6)
	WHERE lock_id = ? AND locked_by = ? AND locked = true`, s.tableName)
	res, err := tx.ExecContext(ctx, updateQ, leaseSeconds, lockID, lockedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to update lease for lock %d: %w", lockID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to update lease for lock %d: rows affected: %w", lockID, err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("failed to update lease for lock %d: not held by this instance", lockID)
	}

	selectQ := fmt.Sprintf(`SELECT lease_expires_at FROM %s WHERE lock_id = ?`, s.tableName)
	var leaseExpiresAt time.Time
	if err := tx.QueryRowContext(ctx, selectQ, lockID).Scan(&leaseExpiresAt); err != nil {
		return nil, fmt.Errorf("failed to update lease for lock %d: %w", lockID, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to update lease for lock %d: commit: %w", lockID, err)
	}

	return &UpdateLeaseResult{
		LeaseExpiresAt: leaseExpiresAt,
	}, nil
}

func (s *mysqlStore) CheckLockStatus(
	ctx context.Context,
	db *sql.DB,
	lockID int64,
) (*LockStatus, error) {
	query := fmt.Sprintf(`SELECT locked, locked_by, lease_expires_at, updated_at FROM %s WHERE lock_id = ?`, s.tableName)
	var status LockStatus

	err := db.QueryRowContext(ctx, query,
		lockID,
	).Scan(
		&status.Locked,
		&status.LockedBy,
		&status.LeaseExpiresAt,
		&status.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("lock %d not found", lockID)
		}
		return nil, fmt.Errorf("check lock status for %d: %w", lockID, err)
	}

	return &status, nil
}

func (s *mysqlStore) CleanupStaleLocks(ctx context.Context, db *sql.DB) (_ []int64, retErr error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("cleanup stale locks: begin tx: %w", err)
	}
	defer func() {
		if retErr != nil {
			retErr = multierr.Append(retErr, tx.Rollback())
		}
	}()

	// MySQL has no RETURNING on UPDATE; lock rows for update, collect their IDs, then clear them.
	selectQ := fmt.Sprintf(`SELECT lock_id FROM %s WHERE locked = true AND lease_expires_at < now(6) FOR UPDATE`, s.tableName)
	rows, err := tx.QueryContext(ctx, selectQ)
	if err != nil {
		return nil, fmt.Errorf("cleanup stale locks: %w", err)
	}
	var cleanedLocks []int64
	for rows.Next() {
		var lockID int64
		if err := rows.Scan(&lockID); err != nil {
			return nil, multierr.Append(fmt.Errorf("scan cleaned lock ID: %w", err), rows.Close())
		}
		cleanedLocks = append(cleanedLocks, lockID)
	}
	if err := rows.Err(); err != nil {
		retErr = multierr.Append(retErr, rows.Close())
		return nil, fmt.Errorf("iterate over cleaned locks: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close cleaned locks rows: %w", err)
	}

	if len(cleanedLocks) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cleanedLocks)), ",")
		updateQ := fmt.Sprintf(`UPDATE %s SET
			locked = false,
			locked_at = NULL,
			locked_by = NULL,
			lease_expires_at = NULL,
			updated_at = now(6)
		WHERE lock_id IN (%s)`, s.tableName, placeholders)
		args := make([]any, len(cleanedLocks))
		for i, id := range cleanedLocks {
			args[i] = id
		}
		if _, err := tx.ExecContext(ctx, updateQ, args...); err != nil {
			return nil, fmt.Errorf("cleanup stale locks: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("cleanup stale locks: commit: %w", err)
	}

	return cleanedLocks, nil
}
