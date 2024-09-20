package deletion

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/grafana/dskit/user"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

type (
	DeleteRequestStatus string
	indexType           string
)

const (
	StatusReceived  DeleteRequestStatus = "received"
	StatusProcessed DeleteRequestStatus = "processed"

	deleteRequestID      indexType = "1"
	deleteRequestDetails indexType = "2"
	cacheGenNum          indexType = "3"

	tempFileSuffix          = ".temp"
	DeleteRequestsTableName = "delete_requests"
)

var ErrDeleteRequestNotFound = errors.New("could not find matching delete requests")

type DeleteRequestsStore interface {
	AddDeleteRequestGroup(ctx context.Context, req []DeleteRequest) ([]DeleteRequest, error)
	GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error)
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error)
	UpdateStatus(ctx context.Context, req DeleteRequest, newStatus DeleteRequestStatus) error
	GetDeleteRequestGroup(ctx context.Context, userID, requestID string) ([]DeleteRequest, error)
	RemoveDeleteRequests(ctx context.Context, req []DeleteRequest) error
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)
	Stop()
	Name() string
}

// deleteRequestsStore provides all the methods required to manage lifecycle of delete request and things related to it.
type deleteRequestsStore struct {
	indexClient index.Client
	now         func() model.Time
}

// NewDeleteStore creates a store for managing delete requests.
func NewDeleteStore(workingDirectory string, indexStorageClient storage.Client) (DeleteRequestsStore, error) {
	indexClient, err := newDeleteRequestsTable(workingDirectory, indexStorageClient)
	if err != nil {
		return nil, err
	}

	return &deleteRequestsStore{
		indexClient: indexClient,
		now:         model.Now,
	}, nil
}

func (ds *deleteRequestsStore) Stop() {
	ds.indexClient.Stop()
}

// AddDeleteRequestGroup creates entries for new delete requests. All passed delete requests will be associated to
// each other by request id
func (ds *deleteRequestsStore) AddDeleteRequestGroup(ctx context.Context, reqs []DeleteRequest) ([]DeleteRequest, error) {
	if len(reqs) == 0 {
		return nil, nil
	}

	createdAt := ds.now()
	writeBatch := ds.indexClient.NewWriteBatch()
	requestID, err := ds.generateID(ctx, reqs[0])
	if err != nil {
		return nil, err
	}

	var results []DeleteRequest
	for i, req := range reqs {
		newReq, err := newRequest(req, requestID, createdAt, i)
		if err != nil {
			return nil, err
		}

		results = append(results, newReq)
		ds.writeDeleteRequest(newReq, writeBatch)
	}

	if err := ds.indexClient.BatchWrite(ctx, writeBatch); err != nil {
		return nil, err
	}

	return results, nil
}

func newRequest(req DeleteRequest, requestID []byte, createdAt model.Time, seqNumber int) (DeleteRequest, error) {
	req.RequestID = string(requestID)
	req.Status = StatusReceived
	req.CreatedAt = createdAt
	req.SequenceNum = int64(seqNumber)
	if err := req.SetQuery(req.Query); err != nil {
		return DeleteRequest{}, err
	}
	return req, nil
}

func (ds *deleteRequestsStore) writeDeleteRequest(req DeleteRequest, writeBatch index.WriteBatch) {
	userIDAndRequestID := backwardCompatibleDeleteRequestHash(req.UserID, req.RequestID, req.SequenceNum)

	// Add an entry with userID, requestID, and sequence number as range key and status as value to make it easy
	// to manage and lookup status. We don't want to set anything in hash key here since we would want to find
	// delete requests by just status
	writeBatch.Add(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID), []byte(StatusReceived))

	// Add another entry with additional details like creation time, time range of delete request and the logQL requests in value
	rangeValue := fmt.Sprintf("%x:%x:%x", int64(ds.now()), int64(req.StartTime), int64(req.EndTime))
	writeBatch.Add(DeleteRequestsTableName, fmt.Sprintf("%s:%s", deleteRequestDetails, userIDAndRequestID), []byte(rangeValue), []byte(req.Query))

	// create a gen number for this result
	writeBatch.Add(DeleteRequestsTableName, fmt.Sprintf("%s:%s", cacheGenNum, req.UserID), []byte{}, generateCacheGenNumber())
}

// backwardCompatibleDeleteRequestHash generates the hash key for a delete request.
// Sequence numbers were added after deletion was in production so any requests made
// before then won't have one. Ensure backward compatibility by treating the 0th
// sequence number as the old format without any number. As a consequence, the 0th
// sequence number will also be ignored for any new delete requests.
func backwardCompatibleDeleteRequestHash(userID, requestID string, sequenceNumber int64) string {
	if sequenceNumber == 0 {
		return fmt.Sprintf("%s:%s", userID, requestID)
	}
	return fmt.Sprintf("%s:%s:%d", userID, requestID, sequenceNumber)
}

func (ds *deleteRequestsStore) generateID(ctx context.Context, req DeleteRequest) ([]byte, error) {
	requestID := generateUniqueID(req.UserID, req.Query)

	for {
		if _, err := ds.GetDeleteRequestGroup(ctx, req.UserID, string(requestID)); err != nil {
			if err == ErrDeleteRequestNotFound {
				return requestID, nil
			}
			return nil, err
		}

		// we have a collision here, lets recreate a new requestID and check for collision
		time.Sleep(time.Millisecond)
		requestID = generateUniqueID(req.UserID, req.Query)
	}
}

// GetDeleteRequestsByStatus returns all delete requests for given status.
func (ds *deleteRequestsStore) GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, index.Query{
		TableName:  DeleteRequestsTableName,
		HashValue:  string(deleteRequestID),
		ValueEqual: []byte(status),
	})
}

// GetAllDeleteRequestsForUser returns all delete requests for a user.
func (ds *deleteRequestsStore) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, index.Query{
		TableName:        DeleteRequestsTableName,
		HashValue:        string(deleteRequestID),
		RangeValuePrefix: []byte(userID),
	})
}

// UpdateStatus updates status of a delete request.
func (ds *deleteRequestsStore) UpdateStatus(ctx context.Context, req DeleteRequest, newStatus DeleteRequestStatus) error {
	userIDAndRequestID := backwardCompatibleDeleteRequestHash(req.UserID, req.RequestID, req.SequenceNum)

	writeBatch := ds.indexClient.NewWriteBatch()
	writeBatch.Add(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID), []byte(newStatus))

	if newStatus == StatusProcessed {
		// remove runtime filtering for deleted data
		writeBatch.Add(DeleteRequestsTableName, fmt.Sprintf("%s:%s", cacheGenNum, req.UserID), []byte{}, generateCacheGenNumber())
	}

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

// GetDeleteRequestGroup returns delete requests with given requestID.
func (ds *deleteRequestsStore) GetDeleteRequestGroup(ctx context.Context, userID, requestID string) ([]DeleteRequest, error) {
	userIDAndRequestID := fmt.Sprintf("%s:%s", userID, requestID)

	deleteRequests, err := ds.queryDeleteRequests(ctx, index.Query{
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

	sort.Slice(deleteRequests, func(i, j int) bool {
		return deleteRequests[i].SequenceNum < deleteRequests[j].SequenceNum
	})

	return deleteRequests, nil
}

func (ds *deleteRequestsStore) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	query := index.Query{TableName: DeleteRequestsTableName, HashValue: fmt.Sprintf("%s:%s", cacheGenNum, userID)}
	ctx = user.InjectOrgID(ctx, userID)

	genNumber := ""
	err := ds.indexClient.QueryPages(ctx, []index.Query{query}, func(_ index.Query, batch index.ReadBatchResult) (shouldContinue bool) {
		itr := batch.Iterator()
		for itr.Next() {
			genNumber = string(itr.Value())
			break
		}
		return false
	})

	if err != nil {
		return "", err
	}

	return genNumber, nil
}

func (ds *deleteRequestsStore) queryDeleteRequests(ctx context.Context, deleteQuery index.Query) ([]DeleteRequest, error) {
	var deleteRequests []DeleteRequest
	var err error
	err = ds.indexClient.QueryPages(ctx, []index.Query{deleteQuery}, func(_ index.Query, batch index.ReadBatchResult) (shouldContinue bool) {
		// No need to lock inside the callback since we run a single index query.
		itr := batch.Iterator()
		for itr.Next() {
			userID, requestID, seqID := splitUserIDAndRequestID(string(itr.RangeValue()))

			var seqNum int64
			seqNum, err = strconv.ParseInt(seqID, 10, 64)
			if err != nil {
				return false
			}

			deleteRequests = append(deleteRequests, DeleteRequest{
				UserID:      userID,
				RequestID:   requestID,
				SequenceNum: seqNum,
				Status:      DeleteRequestStatus(itr.Value()),
			})
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	return ds.deleteRequestsWithDetails(ctx, deleteRequests)
}

func (ds *deleteRequestsStore) deleteRequestsWithDetails(ctx context.Context, partialDeleteRequests []DeleteRequest) ([]DeleteRequest, error) {
	deleteRequests := make([]DeleteRequest, 0, len(partialDeleteRequests))
	for _, group := range partitionByRequestID(partialDeleteRequests) {
		for _, deleteRequest := range group {
			requestWithDetails, err := ds.queryDeleteRequestDetails(ctx, deleteRequest)
			if err != nil {
				return nil, err
			}
			deleteRequests = append(deleteRequests, requestWithDetails)
		}
	}
	return deleteRequests, nil
}

func (ds *deleteRequestsStore) queryDeleteRequestDetails(ctx context.Context, deleteRequest DeleteRequest) (DeleteRequest, error) {
	userIDAndRequestID := backwardCompatibleDeleteRequestHash(deleteRequest.UserID, deleteRequest.RequestID, deleteRequest.SequenceNum)
	deleteRequestQuery := []index.Query{
		{
			TableName: DeleteRequestsTableName,
			HashValue: fmt.Sprintf("%s:%s", deleteRequestDetails, userIDAndRequestID),
		},
	}

	var marshalError error
	var requestWithDetails DeleteRequest
	err := ds.indexClient.QueryPages(ctx, deleteRequestQuery, func(_ index.Query, batch index.ReadBatchResult) (shouldContinue bool) {
		if requestWithDetails, marshalError = unmarshalDeleteRequestDetails(batch.Iterator(), deleteRequest); marshalError != nil {
			return false
		}

		return true
	})
	if err != nil || marshalError != nil {
		return DeleteRequest{}, err
	}

	return requestWithDetails, nil
}

func unmarshalDeleteRequestDetails(itr index.ReadBatchIterator, req DeleteRequest) (DeleteRequest, error) {
	itr.Next()

	requestWithDetails, err := parseDeleteRequestTimestamps(itr.RangeValue(), req)
	if err != nil {
		return DeleteRequest{}, nil
	}

	if err = requestWithDetails.SetQuery(string(itr.Value())); err != nil {
		return DeleteRequest{}, err
	}

	return requestWithDetails, nil
}

// RemoveDeleteRequests the passed delete requests
func (ds *deleteRequestsStore) RemoveDeleteRequests(ctx context.Context, reqs []DeleteRequest) error {
	writeBatch := ds.indexClient.NewWriteBatch()

	for _, r := range reqs {
		ds.removeRequest(r, writeBatch)
	}

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

func (ds *deleteRequestsStore) removeRequest(req DeleteRequest, writeBatch index.WriteBatch) {
	userIDAndRequestID := backwardCompatibleDeleteRequestHash(req.UserID, req.RequestID, req.SequenceNum)
	writeBatch.Delete(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID))

	// Add another entry with additional details like creation time, time range of delete request and selectors in value
	rangeValue := fmt.Sprintf("%x:%x:%x", int64(req.CreatedAt), int64(req.StartTime), int64(req.EndTime))
	writeBatch.Delete(DeleteRequestsTableName, fmt.Sprintf("%s:%s", deleteRequestDetails, userIDAndRequestID), []byte(rangeValue))

	// ensure caches are invalidated
	writeBatch.Add(DeleteRequestsTableName, fmt.Sprintf("%s:%s", cacheGenNum, req.UserID), []byte{}, []byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
}

func (ds *deleteRequestsStore) Name() string {
	return "delete_requests_store"
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
func generateUniqueID(orgID string, query string) []byte {
	uniqueID := fnv.New32()
	_, _ = uniqueID.Write([]byte(orgID))

	timeNow := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeNow, uint64(time.Now().UnixNano()))
	_, _ = uniqueID.Write(timeNow)

	_, _ = uniqueID.Write([]byte(query))

	return encodeUniqueID(uniqueID.Sum32())
}

func encodeUniqueID(t uint32) []byte {
	throughBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(throughBytes, t)
	encodedThroughBytes := make([]byte, 8)
	hex.Encode(encodedThroughBytes, throughBytes)
	return encodedThroughBytes
}

func splitUserIDAndRequestID(rangeValue string) (userID, requestID, seqID string) {
	parts := strings.Split(rangeValue, ":")

	if len(parts) == 2 {
		return parts[0], parts[1], "0"
	}
	return parts[0], parts[1], parts[2]
}

// unsafeGetString is like yolostring but with a meaningful name
func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf)))
}

func generateCacheGenNumber() []byte {
	return []byte(strconv.FormatInt(time.Now().UnixNano(), 10))
}
