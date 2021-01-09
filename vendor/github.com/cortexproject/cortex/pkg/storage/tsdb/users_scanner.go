package tsdb

import (
	"context"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// AllUsers returns true to each call and should be used whenever the UsersScanner should not filter out
// any user due to sharding.
func AllUsers(_ string) (bool, error) {
	return true, nil
}

type UsersScanner struct {
	bucketClient objstore.Bucket
	logger       log.Logger
	isOwned      func(userID string) (bool, error)
}

func NewUsersScanner(bucketClient objstore.Bucket, isOwned func(userID string) (bool, error), logger log.Logger) *UsersScanner {
	return &UsersScanner{
		bucketClient: bucketClient,
		logger:       logger,
		isOwned:      isOwned,
	}
}

// ScanUsers returns a fresh list of users found in the storage, that are not marked for deletion,
// and list of users marked for deletion.
//
// If sharding is enabled, returned lists contains only the users owned by this instance.
func (s *UsersScanner) ScanUsers(ctx context.Context) (users, markedForDeletion []string, err error) {
	err = s.bucketClient.Iter(ctx, "", func(entry string) error {
		users = append(users, strings.TrimSuffix(entry, "/"))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Check users for being owned by instance, and split users into non-deleted and deleted.
	// We do these checks after listing all users, to improve cacheability of Iter (result is only cached at the end of Iter call).
	for ix := 0; ix < len(users); {
		userID := users[ix]

		// Check if it's owned by this instance.
		owned, err := s.isOwned(userID)
		if err != nil {
			level.Warn(s.logger).Log("msg", "unable to check if user is owned by this shard", "user", userID, "err", err)
		} else if !owned {
			users = append(users[:ix], users[ix+1:]...)
			continue
		}

		deletionMarkExists, err := TenantDeletionMarkExists(ctx, s.bucketClient, userID)
		if err != nil {
			level.Warn(s.logger).Log("msg", "unable to check if user is marked for deletion", "user", userID, "err", err)
		} else if deletionMarkExists {
			users = append(users[:ix], users[ix+1:]...)
			markedForDeletion = append(markedForDeletion, userID)
			continue
		}

		ix++
	}

	return users, markedForDeletion, nil
}
