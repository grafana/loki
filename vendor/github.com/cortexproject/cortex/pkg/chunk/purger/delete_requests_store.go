package purger

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

type DeleteRequestStatus string

const (
	StatusReceived     DeleteRequestStatus = "received"
	StatusBuildingPlan DeleteRequestStatus = "buildingPlan"
	StatusDeleting     DeleteRequestStatus = "deleting"
	StatusProcessed    DeleteRequestStatus = "processed"

	separator = "\000" // separator for series selectors in delete requests
)

var (
	pendingDeleteRequestStatuses = []DeleteRequestStatus{StatusReceived, StatusBuildingPlan, StatusDeleting}

	ErrDeleteRequestNotFound = errors.New("could not find matching delete request")
)

// DeleteRequest holds all the details about a delete request
type DeleteRequest struct {
	RequestID string              `json:"request_id"`
	UserID    string              `json:"-"`
	StartTime model.Time          `json:"start_time"`
	EndTime   model.Time          `json:"end_time"`
	Selectors []string            `json:"selectors"`
	Status    DeleteRequestStatus `json:"status"`
	Matchers  [][]*labels.Matcher `json:"-"`
	CreatedAt model.Time          `json:"created_at"`
}

// DeleteStore provides all the methods required to manage lifecycle of delete request and things related to it
type DeleteStore struct {
	cfg         DeleteStoreConfig
	indexClient chunk.IndexClient
}

// DeleteStoreConfig holds configuration for delete store
type DeleteStoreConfig struct {
	Store             string `yaml:"store"`
	RequestsTableName string `yaml:"requests_table_name"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *DeleteStoreConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Store, "deletes.store", "", "Store for keeping delete request")
	f.StringVar(&cfg.RequestsTableName, "deletes.requests-table-name", "delete_requests", "Name of the table which stores delete requests")
}

// NewDeleteStore creates a store for managing delete requests
func NewDeleteStore(cfg DeleteStoreConfig, indexClient chunk.IndexClient) (*DeleteStore, error) {
	ds := DeleteStore{
		cfg:         cfg,
		indexClient: indexClient,
	}

	return &ds, nil
}

// Add creates entries for a new delete request
func (ds *DeleteStore) AddDeleteRequest(ctx context.Context, userID string, startTime, endTime model.Time, selectors []string) error {
	requestID := generateUniqueID(userID, selectors)

	for {
		_, err := ds.GetDeleteRequest(ctx, userID, string(requestID))
		if err != nil {
			if err == ErrDeleteRequestNotFound {
				break
			}
			return err
		}

		// we have a collision here, lets recreate a new requestID and check for collision
		time.Sleep(time.Millisecond)
		requestID = generateUniqueID(userID, selectors)
	}

	// userID, requestID
	userIDAndRequestID := fmt.Sprintf("%s:%s", userID, requestID)

	// Add an entry with userID, requestID as range key and status as value to make it easy to manage and lookup status
	// We don't want to set anything in hash key here since we would want to find delete requests by just status
	writeBatch := ds.indexClient.NewWriteBatch()
	writeBatch.Add(ds.cfg.RequestsTableName, "", []byte(userIDAndRequestID), []byte(StatusReceived))

	// Add another entry with additional details like creation time, time range of delete request and selectors in value
	rangeValue := fmt.Sprintf("%x:%x:%x", int64(model.Now()), int64(startTime), int64(endTime))
	writeBatch.Add(ds.cfg.RequestsTableName, userIDAndRequestID, []byte(rangeValue), []byte(strings.Join(selectors, separator)))

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

// GetDeleteRequestsByStatus returns all delete requests for given status
func (ds *DeleteStore) GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, []chunk.IndexQuery{{TableName: ds.cfg.RequestsTableName, ValueEqual: []byte(status)}})
}

// GetDeleteRequestsForUserByStatus returns all delete requests for a user with given status
func (ds *DeleteStore) GetDeleteRequestsForUserByStatus(ctx context.Context, userID string, status DeleteRequestStatus) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, []chunk.IndexQuery{
		{TableName: ds.cfg.RequestsTableName, RangeValuePrefix: []byte(userID), ValueEqual: []byte(status)},
	})
}

// GetAllDeleteRequestsForUser returns all delete requests for a user
func (ds *DeleteStore) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, []chunk.IndexQuery{
		{TableName: ds.cfg.RequestsTableName, RangeValuePrefix: []byte(userID)},
	})
}

// UpdateStatus updates status of a delete request
func (ds *DeleteStore) UpdateStatus(ctx context.Context, userID, requestID string, newStatus DeleteRequestStatus) error {
	userIDAndRequestID := fmt.Sprintf("%s:%s", userID, requestID)

	writeBatch := ds.indexClient.NewWriteBatch()
	writeBatch.Add(ds.cfg.RequestsTableName, "", []byte(userIDAndRequestID), []byte(newStatus))

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

// GetDeleteRequest returns delete request with given requestID
func (ds *DeleteStore) GetDeleteRequest(ctx context.Context, userID, requestID string) (*DeleteRequest, error) {
	userIDAndRequestID := fmt.Sprintf("%s:%s", userID, requestID)

	deleteRequests, err := ds.queryDeleteRequests(ctx, []chunk.IndexQuery{
		{TableName: ds.cfg.RequestsTableName, RangeValuePrefix: []byte(userIDAndRequestID)},
	})

	if err != nil {
		return nil, err
	}

	if len(deleteRequests) == 0 {
		return nil, ErrDeleteRequestNotFound
	}

	return &deleteRequests[0], nil
}

// GetPendingDeleteRequestsForUser returns all delete requests for a user which are not processed
func (ds *DeleteStore) GetPendingDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	pendingDeleteRequests := []DeleteRequest{}
	for _, status := range pendingDeleteRequestStatuses {
		deleteRequests, err := ds.GetDeleteRequestsForUserByStatus(ctx, userID, status)
		if err != nil {
			return nil, err
		}

		pendingDeleteRequests = append(pendingDeleteRequests, deleteRequests...)
	}

	return pendingDeleteRequests, nil
}

func (ds *DeleteStore) queryDeleteRequests(ctx context.Context, deleteQuery []chunk.IndexQuery) ([]DeleteRequest, error) {
	deleteRequests := []DeleteRequest{}
	err := ds.indexClient.QueryPages(ctx, deleteQuery, func(query chunk.IndexQuery, batch chunk.ReadBatch) (shouldContinue bool) {
		itr := batch.Iterator()
		for itr.Next() {
			userID, requestID := splitUserIDAndRequestID(string(itr.RangeValue()))

			deleteRequests = append(deleteRequests, DeleteRequest{
				UserID:    userID,
				RequestID: requestID,
				Status:    DeleteRequestStatus(itr.Value()),
			})
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	for i, deleteRequest := range deleteRequests {
		deleteRequestQuery := []chunk.IndexQuery{{TableName: ds.cfg.RequestsTableName, HashValue: fmt.Sprintf("%s:%s", deleteRequest.UserID, deleteRequest.RequestID)}}

		var parseError error
		err := ds.indexClient.QueryPages(ctx, deleteRequestQuery, func(query chunk.IndexQuery, batch chunk.ReadBatch) (shouldContinue bool) {
			itr := batch.Iterator()
			itr.Next()

			deleteRequest, err = parseDeleteRequestTimestamps(itr.RangeValue(), deleteRequest)
			if err != nil {
				parseError = err
				return false
			}

			deleteRequest.Selectors = strings.Split(string(itr.Value()), separator)
			deleteRequests[i] = deleteRequest

			return true
		})

		if err != nil {
			return nil, err
		}

		if parseError != nil {
			return nil, parseError
		}
	}

	return deleteRequests, nil
}

func parseDeleteRequestTimestamps(rangeValue []byte, deleteRequest DeleteRequest) (DeleteRequest, error) {
	hexParts := strings.Split(string(rangeValue), ":")
	if len(hexParts) != 3 {
		return deleteRequest, errors.New("invalid key in parsing delete request lookup response")
	}

	createdAt, err := strconv.ParseInt(hexParts[0], 16, 64)
	if err != nil {
		return deleteRequest, err
	}

	from, err := strconv.ParseInt(hexParts[1], 16, 64)
	if err != nil {
		return deleteRequest, err

	}
	through, err := strconv.ParseInt(hexParts[2], 16, 64)
	if err != nil {
		return deleteRequest, err

	}

	deleteRequest.CreatedAt = model.Time(createdAt)
	deleteRequest.StartTime = model.Time(from)
	deleteRequest.EndTime = model.Time(through)

	return deleteRequest, nil
}

// An id is useful in managing delete requests
func generateUniqueID(orgID string, selectors []string) []byte {
	uniqueID := fnv.New32()
	_, _ = uniqueID.Write([]byte(orgID))

	timeNow := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeNow, uint64(time.Now().UnixNano()))
	_, _ = uniqueID.Write(timeNow)

	for _, selector := range selectors {
		_, _ = uniqueID.Write([]byte(selector))
	}

	return encodeUniqueID(uniqueID.Sum32())
}

func encodeUniqueID(t uint32) []byte {
	throughBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(throughBytes, t)
	encodedThroughBytes := make([]byte, 8)
	hex.Encode(encodedThroughBytes, throughBytes)
	return encodedThroughBytes
}

func splitUserIDAndRequestID(rangeValue string) (userID, requestID string) {
	lastIndex := strings.LastIndex(rangeValue, ":")

	userID = rangeValue[:lastIndex]
	requestID = rangeValue[lastIndex+1:]

	return
}
