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
	ListFiles(ctx context.Context, tableName, userID string) ([]IndexFile, error)
	GetFile(ctx context.Context, tableName, userID, fileName string) (io.ReadCloser, error)
	PutFile(ctx context.Context, tableName, userID, fileName string, file io.ReadSeeker) error
	DeleteFile(ctx context.Context, tableName, userID, fileName string) error
	IsFileNotFoundErr(err error) bool
	IsUserIndexSet() bool
}

type indexSet struct {
	client    Client
	userIndex bool
}

// NewIndexSet handles storage operations based on the value of indexSet.userIndex
func NewIndexSet(client Client, userIndex bool) IndexSet {
	return indexSet{
		client:    client,
		userIndex: userIndex,
	}
}

func (i indexSet) validateUserID(userID string) error {
	if i.userIndex && userID == "" {
		return ErrUserIDMustNotBeEmpty
	} else if !i.userIndex && userID != "" {
		return ErrUserIDMustBeEmpty
	}

	return nil
}

func (i indexSet) ListFiles(ctx context.Context, tableName, userID string) ([]IndexFile, error) {
	err := i.validateUserID(userID)
	if err != nil {
		return nil, err
	}

	if i.userIndex {
		return i.client.ListUserFiles(ctx, tableName, userID)
	} else {
		files, _, err := i.client.ListFiles(ctx, tableName)
		return files, err
	}
}

func (i indexSet) GetFile(ctx context.Context, tableName, userID, fileName string) (io.ReadCloser, error) {
	err := i.validateUserID(userID)
	if err != nil {
		return nil, err
	}

	if i.userIndex {
		return i.client.GetUserFile(ctx, tableName, userID, fileName)
	} else {
		return i.client.GetFile(ctx, tableName, fileName)
	}
}

func (i indexSet) PutFile(ctx context.Context, tableName, userID, fileName string, file io.ReadSeeker) error {
	err := i.validateUserID(userID)
	if err != nil {
		return err
	}

	if i.userIndex {
		return i.client.PutUserFile(ctx, tableName, userID, fileName, file)
	} else {
		return i.client.PutFile(ctx, tableName, fileName, file)
	}
}

func (i indexSet) DeleteFile(ctx context.Context, tableName, userID, fileName string) error {
	err := i.validateUserID(userID)
	if err != nil {
		return err
	}

	if i.userIndex {
		return i.client.DeleteUserFile(ctx, tableName, userID, fileName)
	} else {
		return i.client.DeleteFile(ctx, tableName, fileName)
	}
}

func (i indexSet) IsFileNotFoundErr(err error) bool {
	return i.client.IsFileNotFoundErr(err)
}

func (i indexSet) IsUserIndexSet() bool {
	return i.userIndex
}
