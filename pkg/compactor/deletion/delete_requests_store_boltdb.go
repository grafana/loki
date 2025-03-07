package deletion

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
	tempGZFileSuffix        = ".temp.gz"
	DeleteRequestsTableName = "delete_requests"
)

var ErrDeleteRequestNotFound = errors.New("could not find matching delete requests")

// deleteRequestsStoreBoltDB provides all the methods required to manage lifecycle of delete request and things related to it.
type deleteRequestsStoreBoltDB struct {
	indexClient index.Client
}

// newDeleteRequestsStoreBoltDB creates a store for managing delete requests.
func newDeleteRequestsStoreBoltDB(workingDirectory string, indexStorageClient storage.Client) (*deleteRequestsStoreBoltDB, error) {
	indexClient, err := newDeleteRequestsTable(workingDirectory, indexStorageClient)
	if err != nil {
		return nil, err
	}

	return &deleteRequestsStoreBoltDB{
		indexClient: indexClient,
	}, nil
}

func (ds *deleteRequestsStoreBoltDB) Stop() {
	ds.indexClient.Stop()
}

// AddDeleteRequest creates entries for new delete requests. All passed delete requests will be associated to
// each other by request id
func (ds *deleteRequestsStoreBoltDB) AddDeleteRequest(ctx context.Context, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) (string, error) {
	// Generate unique request ID
	requestID := generateUniqueID(userID, query)

	// Use common implementation
	err := ds.addDeleteRequestWithID(ctx, requestID, userID, query, startTime, endTime, shardByInterval)
	if err != nil {
		return "", err
	}

	return requestID, nil
}

func (ds *deleteRequestsStoreBoltDB) addDeleteRequestWithID(ctx context.Context, requestID, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) error {
	reqs := buildRequests(shardByInterval, query, userID, startTime, endTime)
	if len(reqs) == 0 {
		return fmt.Errorf("zero delete requests created")
	}

	createdAt := model.Now()
	writeBatch := ds.indexClient.NewWriteBatch()

	for i, req := range reqs {
		// Use provided requestID
		newReq, err := newRequest(req, requestID, createdAt, i)
		if err != nil {
			return err
		}

		ds.writeDeleteRequest(newReq, writeBatch)
	}

	ds.updateCacheGen(reqs[0].UserID, writeBatch)

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

func (ds *deleteRequestsStoreBoltDB) mergeShardedRequests(ctx context.Context, requestToAdd DeleteRequest, requestsToRemove []DeleteRequest) error {
	writeBatch := ds.indexClient.NewWriteBatch()

	ds.writeDeleteRequest(requestToAdd, writeBatch)

	for _, req := range requestsToRemove {
		ds.removeRequest(req, writeBatch)
	}

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

func newRequest(req DeleteRequest, requestID string, createdAt model.Time, seqNumber int) (DeleteRequest, error) {
	req.RequestID = requestID
	req.Status = StatusReceived
	req.CreatedAt = createdAt
	req.SequenceNum = int64(seqNumber)
	if err := req.SetQuery(req.Query); err != nil {
		return DeleteRequest{}, err
	}
	return req, nil
}

func (ds *deleteRequestsStoreBoltDB) writeDeleteRequest(req DeleteRequest, writeBatch index.WriteBatch) {
	userIDAndRequestID := backwardCompatibleDeleteRequestHash(req.UserID, req.RequestID, req.SequenceNum)

	// Add an entry with userID, requestID, and sequence number as range key and status as value to make it easy
	// to manage and lookup status. We don't want to set anything in hash key here since we would want to find
	// delete requests by just status
	writeBatch.Add(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID), []byte(req.Status))

	// Add another entry with additional details like creation time, time range of delete request and the logQL requests in value
	rangeValue := fmt.Sprintf("%x:%x:%x", int64(req.CreatedAt), int64(req.StartTime), int64(req.EndTime))
	writeBatch.Add(DeleteRequestsTableName, fmt.Sprintf("%s:%s", deleteRequestDetails, userIDAndRequestID), []byte(rangeValue), []byte(req.Query))
}

func (ds *deleteRequestsStoreBoltDB) updateCacheGen(userID string, writeBatch index.WriteBatch) {
	writeBatch.Add(DeleteRequestsTableName, fmt.Sprintf("%s:%s", cacheGenNum, userID), []byte{}, generateCacheGenNumber())
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

func (ds *deleteRequestsStoreBoltDB) generateID(ctx context.Context, req DeleteRequest) (string, error) {
	requestID := generateUniqueID(req.UserID, req.Query)

	for {
		if _, err := ds.GetDeleteRequest(ctx, req.UserID, requestID); err != nil {
			if errors.Is(err, ErrDeleteRequestNotFound) {
				return requestID, nil
			}
			return "", err
		}

		// we have a collision here, lets recreate a new requestID and check for collision
		time.Sleep(time.Millisecond)
		requestID = generateUniqueID(req.UserID, req.Query)
	}
}

// GetUnprocessedShards returns all the unprocessed shards as individual delete requests.
func (ds *deleteRequestsStoreBoltDB) GetUnprocessedShards(ctx context.Context) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, index.Query{
		TableName:  DeleteRequestsTableName,
		HashValue:  string(deleteRequestID),
		ValueEqual: []byte(StatusReceived),
	})
}

// getAllShards returns all the shards as individual delete requests.
func (ds *deleteRequestsStoreBoltDB) getAllShards(ctx context.Context) ([]DeleteRequest, error) {
	return ds.queryDeleteRequests(ctx, index.Query{
		TableName: DeleteRequestsTableName,
		HashValue: string(deleteRequestID),
	})
}

// GetAllRequests returns all the delete requests.
func (ds *deleteRequestsStoreBoltDB) GetAllRequests(ctx context.Context) ([]DeleteRequest, error) {
	deleteGroups, err := ds.getAllShards(ctx)
	if err != nil {
		return nil, err
	}

	deleteRequests := mergeDeletes(deleteGroups)
	return deleteRequests, nil
}

// GetAllDeleteRequestsForUser returns all delete requests for a user.
func (ds *deleteRequestsStoreBoltDB) GetAllDeleteRequestsForUser(ctx context.Context, userID string, _ bool) ([]DeleteRequest, error) {
	deleteGroups, err := ds.queryDeleteRequests(ctx, index.Query{
		TableName:        DeleteRequestsTableName,
		HashValue:        string(deleteRequestID),
		RangeValuePrefix: []byte(userID),
	})
	if err != nil {
		return nil, err
	}

	deleteRequests := mergeDeletes(deleteGroups)
	return deleteRequests, nil
}

// MarkShardAsProcessed marks a delete request shard as processed.
func (ds *deleteRequestsStoreBoltDB) MarkShardAsProcessed(ctx context.Context, req DeleteRequest) error {
	userIDAndRequestID := backwardCompatibleDeleteRequestHash(req.UserID, req.RequestID, req.SequenceNum)

	writeBatch := ds.indexClient.NewWriteBatch()
	writeBatch.Add(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID), []byte(StatusProcessed))

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

// GetDeleteRequest finds and returns delete request with given ID.
func (ds *deleteRequestsStoreBoltDB) GetDeleteRequest(ctx context.Context, userID, requestID string) (DeleteRequest, error) {
	reqGroup, err := ds.getDeleteRequestGroup(ctx, userID, requestID)
	if err != nil {
		return DeleteRequest{}, err
	}

	startTime, endTime, status := mergeData(reqGroup)
	deleteRequest := reqGroup[0]
	deleteRequest.StartTime = startTime
	deleteRequest.EndTime = endTime
	deleteRequest.Status = status

	return deleteRequest, nil
}

// getDeleteRequestGroup returns delete requests with given requestID.
func (ds *deleteRequestsStoreBoltDB) getDeleteRequestGroup(ctx context.Context, userID, requestID string) ([]DeleteRequest, error) {
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

func (ds *deleteRequestsStoreBoltDB) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
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

func (ds *deleteRequestsStoreBoltDB) queryDeleteRequests(ctx context.Context, deleteQuery index.Query) ([]DeleteRequest, error) {
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

func (ds *deleteRequestsStoreBoltDB) deleteRequestsWithDetails(ctx context.Context, partialDeleteRequests []DeleteRequest) ([]DeleteRequest, error) {
	deleteRequests := make([]DeleteRequest, 0, len(partialDeleteRequests))
	for _, deleteRequest := range partialDeleteRequests {
		requestWithDetails, err := ds.queryDeleteRequestDetails(ctx, deleteRequest)
		if err != nil {
			return nil, err
		}
		deleteRequests = append(deleteRequests, requestWithDetails)
	}

	return deleteRequests, nil
}

func (ds *deleteRequestsStoreBoltDB) queryDeleteRequestDetails(ctx context.Context, deleteRequest DeleteRequest) (DeleteRequest, error) {
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

	requestWithDetails.Query = string(itr.Value())

	return requestWithDetails, nil
}

// RemoveDeleteRequest removes the passed delete request
func (ds *deleteRequestsStoreBoltDB) RemoveDeleteRequest(ctx context.Context, userID, requestID string) error {
	reqs, err := ds.getDeleteRequestGroup(ctx, userID, requestID)
	if err != nil {
		return err
	}

	if len(reqs) == 0 {
		return nil
	}
	writeBatch := ds.indexClient.NewWriteBatch()

	for _, r := range reqs {
		ds.removeRequest(r, writeBatch)
	}
	ds.updateCacheGen(reqs[0].UserID, writeBatch)

	return ds.indexClient.BatchWrite(ctx, writeBatch)
}

func (ds *deleteRequestsStoreBoltDB) removeRequest(req DeleteRequest, writeBatch index.WriteBatch) {
	userIDAndRequestID := backwardCompatibleDeleteRequestHash(req.UserID, req.RequestID, req.SequenceNum)
	writeBatch.Delete(DeleteRequestsTableName, string(deleteRequestID), []byte(userIDAndRequestID))

	// Add another entry with additional details like creation time, time range of delete request and selectors in value
	rangeValue := fmt.Sprintf("%x:%x:%x", int64(req.CreatedAt), int64(req.StartTime), int64(req.EndTime))
	writeBatch.Delete(DeleteRequestsTableName, fmt.Sprintf("%s:%s", deleteRequestDetails, userIDAndRequestID), []byte(rangeValue))
}

// MergeShardedRequests merges the sharded requests back to a single request when we are done with processing all the shards
func (ds *deleteRequestsStoreBoltDB) MergeShardedRequests(ctx context.Context) error {
	deleteGroups, err := ds.getAllShards(context.Background())
	if err != nil {
		return err
	}

	slices.SortFunc(deleteGroups, func(a, b DeleteRequest) int {
		return strings.Compare(a.RequestID, b.RequestID)
	})
	deleteRequests := mergeDeletes(deleteGroups)
	for _, req := range deleteRequests {
		// do not consider requests which do not have an id. Request ID won't be set in some tests or there is a bug in our code for loading requests.
		if req.RequestID == "" {
			level.Error(util_log.Logger).Log("msg", "skipped considering request without an id for merging its shards",
				"user_id", req.UserID,
				"start_time", req.StartTime.Unix(),
				"end_time", req.EndTime.Unix(),
				"query", req.Query,
			)
			continue
		}
		// do not do anything if we are not done with processing all the shards or the number of shards is 1
		if req.Status != StatusProcessed {
			continue
		}

		var idxStart, idxEnd int
		for i := range deleteGroups {
			if req.RequestID == deleteGroups[i].RequestID {
				idxStart = i
				break
			}
		}

		for i := len(deleteGroups) - 1; i > 0; i-- {
			if req.RequestID == deleteGroups[i].RequestID {
				idxEnd = i
				break
			}
		}

		// do not do anything if the number of shards is 1
		if idxStart == idxEnd {
			continue
		}
		reqShards := deleteGroups[idxStart : idxEnd+1]

		level.Info(util_log.Logger).Log("msg", "merging sharded request",
			"request_id", req.RequestID,
			"num_shards", len(reqShards),
			"start_time", req.StartTime.Unix(),
			"end_time", req.EndTime.Unix(),
		)
		if err := ds.mergeShardedRequests(ctx, req, reqShards); err != nil {
			return err
		}
	}

	return nil
}

func (ds *deleteRequestsStoreBoltDB) getAllData(ctx context.Context) ([]DeleteRequest, []userCacheGen, error) {
	shards, err := ds.getAllShards(ctx)
	if err != nil {
		return nil, nil, err
	}

	cacheGenNums := map[string]string{}
	var userCacheGens []userCacheGen
	for _, shard := range shards {
		if _, ok := cacheGenNums[shard.UserID]; ok {
			continue
		}

		cacheGen, err := ds.GetCacheGenerationNumber(ctx, shard.UserID)
		if err != nil {
			return nil, nil, err
		}

		if cacheGen == "" {
			cacheGen = strconv.FormatInt(time.Now().UnixNano(), 10)
		}

		cacheGenNums[shard.UserID] = cacheGen
		userCacheGens = append(userCacheGens, userCacheGen{
			userID:   shard.UserID,
			cacheGen: cacheGen,
		})
	}

	return shards, userCacheGens, nil
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
func generateUniqueID(orgID string, query string) string {
	uniqueID := fnv.New32()
	_, _ = uniqueID.Write([]byte(orgID))

	timeNow := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeNow, uint64(time.Now().UnixNano()))
	_, _ = uniqueID.Write(timeNow)

	_, _ = uniqueID.Write([]byte(query))

	return string(encodeUniqueID(uniqueID.Sum32()))
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
	return *((*string)(unsafe.Pointer(&buf))) // #nosec G103 -- we know the string is not mutated
}

func generateCacheGenNumber() []byte {
	return []byte(strconv.FormatInt(time.Now().UnixNano(), 10))
}
