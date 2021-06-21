package deletion

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/chunk"
)

type (
	DeleteRequestStatus string
	indexType           string
)

const (
	StatusReceived  DeleteRequestStatus = "received"
	StatusProcessed DeleteRequestStatus = "processed"

	separator = "\000" // separator for series selectors in delete requests

	deleteRequestID      indexType = "1"
	deleteRequestDetails indexType = "2"

	tempFileSuffix          = ".temp"
	DeleteRequestsTableName = "delete_requests"
)

var ErrDeleteRequestNotFound = errors.New("could not find matching delete request")

type DeleteRequestsStore interface {
	AddDeleteRequest(ctx context.Context, userID string, startTime, endTime model.Time, selectors []string) error
	GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error)
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error)
	UpdateStatus(ctx context.Context, userID, requestID string, newStatus DeleteRequestStatus) error
	GetDeleteRequest(ctx context.Context, userID, requestID string) (*DeleteRequest, error)
	RemoveDeleteRequest(ctx context.Context, userID, requestID string, createdAt, startTime, endTime model.Time) error
	Stop()
}

// deleteRequestsStore provides all the methods required to manage lifecycle of delete request and things related to it.
type deleteRequestsStore struct {
	indexClient chunk.IndexClient
}

// NewDeleteStore creates a store for managing delete requests.
func NewDeleteStore(workingDirectory string, objectClient chunk.ObjectClient) (DeleteRequestsStore, error) {
	indexClient, err := newDeleteRequestsTable(workingDirectory, objectClient)
	if err != nil {
		return nil, err
	}

	return &deleteRequestsStore{indexClient: indexClient}, nil
}

func (ds *deleteRequestsStore) Stop() {
	ds.indexClient.Stop()
}

// AddDeleteRequest creates entries for a new delete request.
func (ds *deleteRequestsStore) AddDeleteRequest(ctx context.Context, userID string, startTime, endTime model.Time, selectors []string) error {
	_, err := ds.addDeleteRequest(ctx, userID, model.Now(), startTime, endTime, selectors)
	return err
}

// addDeleteRequest is also used for tests to create delete requests with different createdAt time.
func (ds *deleteRequestsStore) addDeleteRequest(ctx context.Context, userID string, createdAt, startTime, endTime model.Time, selectors []string) ([]byte, error) {
	requestID := generateUniqueID(userID, selectors)

	for {
		_, err := ds.GetDeleteRequest(ctx, userID, string(requestID))
		if err != nil {
			if err == ErrDeleteRequestNotFound {
				break
			}
			return nil, err
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
	writeBatch.Add(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID), []byte(StatusReceived))

	// Add another entry with additional details like creation time, time range of delete request and selectors in value
	rangeValue := fmt.Sprintf("%x:%x:%x", int64(createdAt), int64(startTime), int64(endTime))
	writeBatch.Add(DeleteRequestsTableName, fmt.Sprintf("%s:%s", deleteRequestDetails, userIDAndRequestID),
		[]byte(rangeValue), []byte(strings.Join(selectors, separator)))

	err := ds.indexClient.BatchWrite(ctx, writeBatch)
	if err != nil {
		return nil, err
	}

	return requestID, nil
}

// GetDeleteRequestsByStatus returns all delete requests for given status.
func (ds *deleteRequestsStore) GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, chunk.IndexQuery{
		TableName:  DeleteRequestsTableName,
		HashValue:  string(deleteRequestID),
		ValueEqual: []byte(status),
	})
}

// GetAllDeleteRequestsForUser returns all delete requests for a user.
func (ds *deleteRequestsStore) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, chunk.IndexQuery{
		TableName:        DeleteRequestsTableName,
		HashValue:        string(deleteRequestID),
		RangeValuePrefix: []byte(userID),
	})
}

// UpdateStatus updates status of a delete request.
func (ds *deleteRequestsStore) UpdateStatus(ctx context.Context, userID, requestID string, newStatus DeleteRequestStatus) error {
	userIDAndRequestID := fmt.Sprintf("%s:%s", userID, requestID)

	writeBatch := ds.indexClient.NewWriteBatch()
	writeBatch.Add(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID), []byte(newStatus))

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

// GetDeleteRequest returns delete request with given requestID.
func (ds *deleteRequestsStore) GetDeleteRequest(ctx context.Context, userID, requestID string) (*DeleteRequest, error) {
	userIDAndRequestID := fmt.Sprintf("%s:%s", userID, requestID)

	deleteRequests, err := ds.queryDeleteRequests(ctx, chunk.IndexQuery{
		TableName:        DeleteRequestsTableName,
		HashValue:        string(deleteRequestID),
		RangeValuePrefix: []byte(userIDAndRequestID),
	})
	if err != nil {
		return nil, err
	}

	if len(deleteRequests) == 0 {
		return nil, ErrDeleteRequestNotFound
	}

	return &deleteRequests[0], nil
}

func (ds *deleteRequestsStore) queryDeleteRequests(ctx context.Context, deleteQuery chunk.IndexQuery) ([]DeleteRequest, error) {
	deleteRequests := []DeleteRequest{}
	// No need to lock inside the callback since we run a single index query.
	err := ds.indexClient.QueryPages(ctx, []chunk.IndexQuery{deleteQuery}, func(query chunk.IndexQuery, batch chunk.ReadBatch) (shouldContinue bool) {
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
		deleteRequestQuery := []chunk.IndexQuery{
			{
				TableName: DeleteRequestsTableName,
				HashValue: fmt.Sprintf("%s:%s:%s", deleteRequestDetails, deleteRequest.UserID, deleteRequest.RequestID),
			},
		}

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

// RemoveDeleteRequest removes a delete request
func (ds *deleteRequestsStore) RemoveDeleteRequest(ctx context.Context, userID, requestID string, createdAt, startTime, endTime model.Time) error {
	userIDAndRequestID := fmt.Sprintf("%s:%s", userID, requestID)

	writeBatch := ds.indexClient.NewWriteBatch()
	writeBatch.Delete(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID))

	// Add another entry with additional details like creation time, time range of delete request and selectors in value
	rangeValue := fmt.Sprintf("%x:%x:%x", int64(createdAt), int64(startTime), int64(endTime))
	writeBatch.Delete(DeleteRequestsTableName, fmt.Sprintf("%s:%s", deleteRequestDetails, userIDAndRequestID),
		[]byte(rangeValue))

	return ds.indexClient.BatchWrite(ctx, writeBatch)
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

// unsafeGetString is like yolostring but with a meaningful name
func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf)))
}
