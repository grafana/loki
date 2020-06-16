package compactor

import (
	"context"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/thanos-io/thanos/pkg/objstore"
)

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

// ScanUsers returns a fresh list of users found in the storage. If sharding is enabled,
// the returned list contains only the users owned by this instance.
func (s *UsersScanner) ScanUsers(ctx context.Context) ([]string, error) {
	var users []string

	err := s.bucketClient.Iter(ctx, "", func(entry string) error {
		userID := strings.TrimSuffix(entry, "/")

		// Check if it's owned by this instance.
		owned, err := s.isOwned(userID)
		if err != nil {
			level.Warn(s.logger).Log("msg", "unable to check if user is owned by this shard", "user", userID, "err", err)
		} else if !owned {
			return nil
		}

		users = append(users, userID)
		return nil
	})

	return users, err
}
