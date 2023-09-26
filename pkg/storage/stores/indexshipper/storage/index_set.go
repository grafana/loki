package storage

import (
	"context"
	"errors"
	"io"
)

var (
	ErrUserIDMustNotBeEmpty = errors.New("userID must not be empty")
	ErrUserIDMustBeEmpty    = errors.New("userID must be empty")
)

// IndexSet provides storage operations for user or common index tables.
type IndexSet interface {
	RefreshIndexTableCache(ctx context.Context, tableName string)
	ListFiles(ctx context.Context, tableName, userID string, bypassCache bool) ([]IndexFile, error)
	GetFile(ctx context.Context, tableName, userID, fileName string) (io.ReadCloser, error)
	PutFile(ctx context.Context, tableName, userID, fileName string, file io.ReadSeeker) error
	DeleteFile(ctx context.Context, tableName, userID, fileName string) error
	IsFileNotFoundErr(err error) bool
	IsUserBasedIndexSet() bool
}

type indexSet struct {
	client         Client
	userBasedIndex bool
}

// NewIndexSet handles storage operations based on the value of indexSet.userBasedIndex
func NewIndexSet(client Client, userBasedIndex bool) IndexSet {
	return indexSet{
		client:         client,
		userBasedIndex: userBasedIndex,
	}
}

func (i indexSet) validateUserID(userID string) error {
	if i.userBasedIndex && userID == "" {
		return ErrUserIDMustNotBeEmpty
	} else if !i.userBasedIndex && userID != "" {
		return ErrUserIDMustBeEmpty
	}

	return nil
}

func (i indexSet) RefreshIndexTableCache(ctx context.Context, tableName string) {
	i.client.RefreshIndexTableCache(ctx, tableName)
}

func (i indexSet) ListFiles(ctx context.Context, tableName, userID string, bypassCache bool) ([]IndexFile, error) {
	err := i.validateUserID(userID)
	if err != nil {
		return nil, err
	}

	if i.userBasedIndex {
		return i.client.ListUserFiles(ctx, tableName, userID, bypassCache)
	}

	files, _, err := i.client.ListFiles(ctx, tableName, bypassCache)
	return files, err
}

func (i indexSet) GetFile(ctx context.Context, tableName, userID, fileName string) (io.ReadCloser, error) {
	err := i.validateUserID(userID)
	if err != nil {
		return nil, err
	}

	if i.userBasedIndex {
		return i.client.GetUserFile(ctx, tableName, userID, fileName)
	}

	return i.client.GetFile(ctx, tableName, fileName)
}

func (i indexSet) PutFile(ctx context.Context, tableName, userID, fileName string, file io.ReadSeeker) error {
	err := i.validateUserID(userID)
	if err != nil {
		return err
	}

	if i.userBasedIndex {
		return i.client.PutUserFile(ctx, tableName, userID, fileName, file)
	}

	return i.client.PutFile(ctx, tableName, fileName, file)
}

func (i indexSet) DeleteFile(ctx context.Context, tableName, userID, fileName string) error {
	err := i.validateUserID(userID)
	if err != nil {
		return err
	}

	if i.userBasedIndex {
		return i.client.DeleteUserFile(ctx, tableName, userID, fileName)
	}

	return i.client.DeleteFile(ctx, tableName, fileName)
}

func (i indexSet) IsFileNotFoundErr(err error) bool {
	return i.client.IsFileNotFoundErr(err)
}

func (i indexSet) IsUserBasedIndexSet() bool {
	return i.userBasedIndex
}
