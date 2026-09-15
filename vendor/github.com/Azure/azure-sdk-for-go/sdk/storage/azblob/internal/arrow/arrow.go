// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package arrow

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/shared"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const (
	// ArrowContentType is the Content-Type for Apache Arrow IPC stream responses.
	ArrowContentType = "application/vnd.apache.arrow.stream"

	// ArrowAcceptHeader is the Accept header value to request Arrow format.
	ArrowAcceptHeader = ArrowContentType

	// resourceTypeBlobPrefix is the ResourceType value that identifies a virtual directory prefix
	// in hierarchy listing responses. In XML, prefixes are separate <BlobPrefix> elements;
	// in Arrow, all rows share the same schema and are distinguished by this field value.
	resourceTypeBlobPrefix = "blobprefix"
)

// HandleFlatListResponse parses an Arrow IPC stream response into a ContainerClientListBlobFlatSegmentResponse.
func HandleFlatListResponse(resp *http.Response) (generated.ContainerClientListBlobFlatSegmentResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	result := generated.ContainerClientListBlobFlatSegmentResponse{}

	h := extractResponseHeaders(resp)
	result.ClientRequestID = h.ClientRequestID
	result.ContentType = h.ContentType
	result.Date = h.Date
	result.RequestID = h.RequestID
	result.Version = h.Version

	items, nextMarker, err := parseArrowStream(resp.Body)
	if err != nil {
		return result, fmt.Errorf("failed to parse Arrow IPC response: %s", err.Error())
	}

	result.ListBlobsFlatSegmentResponse = generated.ListBlobsFlatSegmentResponse{
		Segment: &generated.BlobFlatListSegment{
			BlobItems: items,
		},
		NextMarker: nextMarker,
	}

	return result, nil
}

// HandleHierarchyListResponse parses an Arrow IPC stream response into a ContainerClientListBlobHierarchySegmentResponse.
func HandleHierarchyListResponse(resp *http.Response) (generated.ContainerClientListBlobHierarchySegmentResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	result := generated.ContainerClientListBlobHierarchySegmentResponse{}

	h := extractResponseHeaders(resp)
	result.ClientRequestID = h.ClientRequestID
	result.ContentType = h.ContentType
	result.Date = h.Date
	result.RequestID = h.RequestID
	result.Version = h.Version

	items, nextMarker, err := parseArrowStream(resp.Body)
	if err != nil {
		return result, fmt.Errorf("failed to parse Arrow IPC response: %s", err.Error())
	}

	// Separate blob items from blob prefixes based on ResourceType field.
	var blobItems []*generated.BlobItem
	var blobPrefixes []*generated.BlobPrefix
	for _, item := range items {
		if item.Properties != nil && item.Properties.ResourceType != nil && *item.Properties.ResourceType == resourceTypeBlobPrefix {
			blobPrefixes = append(blobPrefixes, &generated.BlobPrefix{
				Name:       item.Name,
				Properties: item.Properties,
			})
		} else {
			blobItems = append(blobItems, item)
		}
	}

	result.ListBlobsHierarchySegmentResponse = generated.ListBlobsHierarchySegmentResponse{
		Segment: &generated.BlobHierarchyListSegment{
			BlobItems:    blobItems,
			BlobPrefixes: blobPrefixes,
		},
		NextMarker: nextMarker,
	}

	return result, nil
}

type responseHeaders struct {
	ClientRequestID *string
	ContentType     *string
	Date            *time.Time
	RequestID       *string
	Version         *string
}

func extractResponseHeaders(resp *http.Response) responseHeaders {
	var h responseHeaders
	if val := resp.Header.Get(shared.HeaderXmsClientRequestID); val != "" {
		h.ClientRequestID = &val
	}
	if val := resp.Header.Get(shared.HeaderContentType); val != "" {
		h.ContentType = &val
	}
	if val := resp.Header.Get(shared.HeaderDate); val != "" {
		if t, err := time.Parse(time.RFC1123, val); err == nil {
			h.Date = &t
		}
	}
	if val := resp.Header.Get(shared.HeaderXmsRequestID); val != "" {
		h.RequestID = &val
	}
	if val := resp.Header.Get(shared.HeaderXmsVersion); val != "" {
		h.Version = &val
	}
	return h
}

// parseArrowStream reads an Arrow IPC stream and converts it to BlobItems.
// Columns are resolved once per record batch to avoid per-row map lookups and type assertions.
// BlobItem/BlobProperties structs are batch-allocated to reduce heap allocations.
func parseArrowStream(body io.Reader) ([]*generated.BlobItem, *string, error) {
	reader, err := ipc.NewReader(body, ipc.WithAllocator(memory.DefaultAllocator))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Arrow IPC reader: %s", err.Error())
	}
	defer reader.Release()

	nextMarker, numRecords := readSchemaMetadata(reader.Schema())
	colIdx := buildColumnIndex(reader.Schema())
	items := make([]*generated.BlobItem, 0, numRecords)

	for reader.Next() {
		rec := reader.RecordBatch()
		rows := int(rec.NumRows())

		cols := resolveColumns(rec, colIdx)

		blobs := make([]generated.BlobItem, rows)
		props := make([]generated.BlobProperties, rows)

		for row := 0; row < rows; row++ {
			blobs[row].Properties = &props[row]
			cols.populate(&blobs[row], row)
			items = append(items, &blobs[row])
		}
	}

	if err := reader.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading Arrow record batches: %s", err.Error())
	}

	return items, nextMarker, nil
}

func readSchemaMetadata(schema *arrow.Schema) (*string, int) {
	var nextMarker *string
	var numRecords int
	if md := schema.Metadata(); md.Len() > 0 {
		if idx := md.FindKey("NextMarker"); idx >= 0 {
			val := md.Values()[idx]
			if val != "" {
				nextMarker = &val
			}
		}
		if idx := md.FindKey("NumberOfRecords"); idx >= 0 {
			if n, err := strconv.Atoi(md.Values()[idx]); err == nil {
				numRecords = n
			}
		}
	}
	return nextMarker, numRecords
}

// columnIndex maps column names to their indices in the record batch for O(1) lookup.
type columnIndex map[string]int

func buildColumnIndex(schema *arrow.Schema) columnIndex {
	idx := make(columnIndex, len(schema.Fields()))
	for i, f := range schema.Fields() {
		idx[f.Name] = i
	}
	return idx
}

// ---------------------------------------------------------------------------
// Column wrapper types — resolved once per record batch, accessed per row.
// ---------------------------------------------------------------------------

type tsColumn struct {
	arr  *array.Timestamp
	unit arrow.TimeUnit
}

func (c tsColumn) at(row int) *time.Time {
	if c.arr == nil || c.arr.IsNull(row) {
		return nil
	}
	t := c.arr.Value(row).ToTime(c.unit)
	return &t
}

type i64Column struct {
	u *array.Uint64
	i *array.Int64
}

func (c i64Column) at(row int) *int64 {
	if c.u != nil {
		if c.u.IsNull(row) {
			return nil
		}
		v := int64(c.u.Value(row))
		return &v
	}
	if c.i != nil {
		if c.i.IsNull(row) {
			return nil
		}
		v := c.i.Value(row)
		return &v
	}
	return nil
}

type i32Column struct {
	u *array.Uint64
	i *array.Int32
}

func (c i32Column) at(row int) *int32 {
	if c.u != nil {
		if c.u.IsNull(row) {
			return nil
		}
		v := int32(c.u.Value(row))
		return &v
	}
	if c.i != nil {
		if c.i.IsNull(row) {
			return nil
		}
		v := c.i.Value(row)
		return &v
	}
	return nil
}

type mapColumn struct {
	m      *array.Map
	keys   *array.String
	values *array.String
}

func (c mapColumn) at(row int) map[string]*string {
	if c.m == nil || c.m.IsNull(row) {
		return nil
	}
	start := int(c.m.Offsets()[row])
	end := int(c.m.Offsets()[row+1])
	if start == end {
		return nil
	}
	result := make(map[string]*string, end-start)
	for i := start; i < end; i++ {
		k := c.keys.Value(i)
		if c.values.IsNull(i) {
			result[k] = nil
		} else {
			v := c.values.Value(i)
			result[k] = &v
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Column resolvers — extract the typed array from a record batch once.
// All use two-value type assertions so a schema mismatch returns nil, never panics.
// ---------------------------------------------------------------------------

func resolveString(rec arrow.RecordBatch, colIdx columnIndex, name string) *array.String {
	idx, ok := colIdx[name]
	if !ok {
		return nil
	}
	a, _ := rec.Column(idx).(*array.String)
	return a
}

func resolveBool(rec arrow.RecordBatch, colIdx columnIndex, name string) *array.Boolean {
	idx, ok := colIdx[name]
	if !ok {
		return nil
	}
	a, _ := rec.Column(idx).(*array.Boolean)
	return a
}

func resolveTimestamp(rec arrow.RecordBatch, colIdx columnIndex, name string) tsColumn {
	idx, ok := colIdx[name]
	if !ok {
		return tsColumn{}
	}
	arr, ok := rec.Column(idx).(*array.Timestamp)
	if !ok {
		return tsColumn{}
	}
	tt, ok := arr.DataType().(*arrow.TimestampType)
	if !ok {
		return tsColumn{}
	}
	return tsColumn{arr: arr, unit: tt.Unit}
}

func resolveInt64(rec arrow.RecordBatch, colIdx columnIndex, name string) i64Column {
	idx, ok := colIdx[name]
	if !ok {
		return i64Column{}
	}
	col := rec.Column(idx)
	if u, ok := col.(*array.Uint64); ok {
		return i64Column{u: u}
	}
	if i, ok := col.(*array.Int64); ok {
		return i64Column{i: i}
	}
	return i64Column{}
}

func resolveInt32(rec arrow.RecordBatch, colIdx columnIndex, name string) i32Column {
	idx, ok := colIdx[name]
	if !ok {
		return i32Column{}
	}
	col := rec.Column(idx)
	if u, ok := col.(*array.Uint64); ok {
		return i32Column{u: u}
	}
	if i, ok := col.(*array.Int32); ok {
		return i32Column{i: i}
	}
	return i32Column{}
}

func resolveMapCol(rec arrow.RecordBatch, colIdx columnIndex, name string) mapColumn {
	idx, ok := colIdx[name]
	if !ok {
		return mapColumn{}
	}
	m, ok := rec.Column(idx).(*array.Map)
	if !ok {
		return mapColumn{}
	}
	keys, ok := m.Keys().(*array.String)
	if !ok {
		return mapColumn{}
	}
	values, ok := m.Items().(*array.String)
	if !ok {
		return mapColumn{}
	}
	return mapColumn{m: m, keys: keys, values: values}
}

// ---------------------------------------------------------------------------
// blobColumns holds all typed arrays for a single record batch.
// ---------------------------------------------------------------------------

type blobColumns struct {
	name             *array.String
	deleted          *array.Boolean
	snapshot         *array.String
	versionID        *array.String
	isCurrentVersion *array.Boolean
	hasVersionsOnly  *array.Boolean
	metadata         mapColumn
	orMetadata       mapColumn
	tags             mapColumn

	creationTime              tsColumn
	lastModified              tsColumn
	etag                      *array.String
	contentLength             i64Column
	contentType               *array.String
	contentEncoding           *array.String
	contentLanguage           *array.String
	contentDisposition        *array.String
	cacheControl              *array.String
	contentMD5                *array.String
	blobType                  *array.String
	accessTier                *array.String
	accessTierInferred        *array.Boolean
	accessTierChangeTime      tsColumn
	smartAccessTier           *array.String
	leaseState                *array.String
	leaseStatus               *array.String
	leaseDuration             *array.String
	serverEncrypted           *array.Boolean
	customerProvidedKeySHA256 *array.String
	encryptionScope           *array.String
	incrementalCopy           *array.Boolean
	isSealed                  *array.Boolean
	archiveStatus             *array.String
	rehydratePriority         *array.String
	copyID                    *array.String
	copyStatus                *array.String
	copySource                *array.String
	copyProgress              *array.String
	copyCompletionTime        tsColumn
	copyStatusDescription     *array.String
	destinationSnapshot       *array.String
	immutabilityPolicyExpiry  tsColumn
	immutabilityPolicyMode    *array.String
	legalHold                 *array.Boolean
	deletedTime               tsColumn
	remainingRetentionDays    i32Column
	lastAccessedOn            tsColumn
	tagCount                  i32Column
	blobSequenceNumber        i64Column
	resourceType              *array.String
}

func resolveColumns(rec arrow.RecordBatch, colIdx columnIndex) blobColumns {
	return blobColumns{
		name:                      resolveString(rec, colIdx, "Name"),
		deleted:                   resolveBool(rec, colIdx, "Deleted"),
		snapshot:                  resolveString(rec, colIdx, "Snapshot"),
		versionID:                 resolveString(rec, colIdx, "VersionId"),
		isCurrentVersion:          resolveBool(rec, colIdx, "IsCurrentVersion"),
		hasVersionsOnly:           resolveBool(rec, colIdx, "HasVersionsOnly"),
		metadata:                  resolveMapCol(rec, colIdx, "Metadata"),
		orMetadata:                resolveMapCol(rec, colIdx, "OrMetadata"),
		tags:                      resolveMapCol(rec, colIdx, "Tags"),
		creationTime:              resolveTimestamp(rec, colIdx, "Creation-Time"),
		lastModified:              resolveTimestamp(rec, colIdx, "Last-Modified"),
		etag:                      resolveString(rec, colIdx, "Etag"),
		contentLength:             resolveInt64(rec, colIdx, "Content-Length"),
		contentType:               resolveString(rec, colIdx, "Content-Type"),
		contentEncoding:           resolveString(rec, colIdx, "Content-Encoding"),
		contentLanguage:           resolveString(rec, colIdx, "Content-Language"),
		contentDisposition:        resolveString(rec, colIdx, "Content-Disposition"),
		cacheControl:              resolveString(rec, colIdx, "Cache-Control"),
		contentMD5:                resolveString(rec, colIdx, "Content-MD5"),
		blobType:                  resolveString(rec, colIdx, "BlobType"),
		accessTier:                resolveString(rec, colIdx, "AccessTier"),
		accessTierInferred:        resolveBool(rec, colIdx, "AccessTierInferred"),
		accessTierChangeTime:      resolveTimestamp(rec, colIdx, "AccessTierChangeTime"),
		smartAccessTier:           resolveString(rec, colIdx, "SmartAccessTier"),
		leaseState:                resolveString(rec, colIdx, "LeaseState"),
		leaseStatus:               resolveString(rec, colIdx, "LeaseStatus"),
		leaseDuration:             resolveString(rec, colIdx, "LeaseDuration"),
		serverEncrypted:           resolveBool(rec, colIdx, "ServerEncrypted"),
		customerProvidedKeySHA256: resolveString(rec, colIdx, "CustomerProvidedKeySha256"),
		encryptionScope:           resolveString(rec, colIdx, "EncryptionScope"),
		incrementalCopy:           resolveBool(rec, colIdx, "IncrementalCopy"),
		isSealed:                  resolveBool(rec, colIdx, "Sealed"),
		archiveStatus:             resolveString(rec, colIdx, "ArchiveStatus"),
		rehydratePriority:         resolveString(rec, colIdx, "RehydratePriority"),
		copyID:                    resolveString(rec, colIdx, "CopyId"),
		copyStatus:                resolveString(rec, colIdx, "CopyStatus"),
		copySource:                resolveString(rec, colIdx, "CopySource"),
		copyProgress:              resolveString(rec, colIdx, "CopyProgress"),
		copyCompletionTime:        resolveTimestamp(rec, colIdx, "CopyCompletionTime"),
		copyStatusDescription:     resolveString(rec, colIdx, "CopyStatusDescription"),
		destinationSnapshot:       resolveString(rec, colIdx, "CopyDestinationSnapshot"),
		immutabilityPolicyExpiry:  resolveTimestamp(rec, colIdx, "ImmutabilityPolicyUntilDate"),
		immutabilityPolicyMode:    resolveString(rec, colIdx, "ImmutabilityPolicyMode"),
		legalHold:                 resolveBool(rec, colIdx, "LegalHold"),
		deletedTime:               resolveTimestamp(rec, colIdx, "DeletedTime"),
		remainingRetentionDays:    resolveInt32(rec, colIdx, "RemainingRetentionDays"),
		lastAccessedOn:            resolveTimestamp(rec, colIdx, "LastAccessTime"),
		tagCount:                  resolveInt32(rec, colIdx, "TagCount"),
		blobSequenceNumber:        resolveInt64(rec, colIdx, "x-ms-blob-sequence-number"),
		resourceType:              resolveString(rec, colIdx, "ResourceType"),
	}
}

func (c *blobColumns) populate(item *generated.BlobItem, row int) {
	item.Name = strAt(c.name, row)
	item.Deleted = boolAt(c.deleted, row)
	item.Snapshot = strAt(c.snapshot, row)
	item.VersionID = strAt(c.versionID, row)
	item.IsCurrentVersion = boolAt(c.isCurrentVersion, row)
	item.HasVersionsOnly = boolAt(c.hasVersionsOnly, row)
	item.Metadata = c.metadata.at(row)
	item.OrMetadata = c.orMetadata.at(row)

	if tags := c.tags.at(row); tags != nil {
		var blobTags []*generated.BlobTag
		for k, v := range tags {
			key := k
			blobTags = append(blobTags, &generated.BlobTag{Key: &key, Value: v})
		}
		item.BlobTags = &generated.BlobTags{BlobTagSet: blobTags}
	}

	p := item.Properties
	p.CreationTime = c.creationTime.at(row)
	p.LastModified = c.lastModified.at(row)
	p.ETag = etagAt(c.etag, row)
	p.ContentLength = c.contentLength.at(row)
	p.ContentType = strAt(c.contentType, row)
	p.ContentEncoding = strAt(c.contentEncoding, row)
	p.ContentLanguage = strAt(c.contentLanguage, row)
	p.ContentDisposition = strAt(c.contentDisposition, row)
	p.CacheControl = strAt(c.cacheControl, row)
	p.ContentMD5 = base64At(c.contentMD5, row)
	p.BlobType = enumAt[generated.BlobType](c.blobType, row)
	p.AccessTier = enumAt[generated.AccessTier](c.accessTier, row)
	p.AccessTierInferred = boolAt(c.accessTierInferred, row)
	p.AccessTierChangeTime = c.accessTierChangeTime.at(row)
	p.SmartAccessTier = enumAt[generated.AccessTier](c.smartAccessTier, row)
	p.LeaseState = enumAt[generated.LeaseStateType](c.leaseState, row)
	p.LeaseStatus = enumAt[generated.LeaseStatusType](c.leaseStatus, row)
	p.LeaseDuration = enumAt[generated.LeaseDurationType](c.leaseDuration, row)
	p.ServerEncrypted = boolAt(c.serverEncrypted, row)
	p.CustomerProvidedKeySHA256 = strAt(c.customerProvidedKeySHA256, row)
	p.EncryptionScope = strAt(c.encryptionScope, row)
	p.IncrementalCopy = boolAt(c.incrementalCopy, row)
	p.IsSealed = boolAt(c.isSealed, row)
	p.ArchiveStatus = enumAt[generated.ArchiveStatus](c.archiveStatus, row)
	p.RehydratePriority = enumAt[generated.RehydratePriority](c.rehydratePriority, row)
	p.CopyID = strAt(c.copyID, row)
	p.CopyStatus = enumAt[generated.CopyStatusType](c.copyStatus, row)
	p.CopySource = strAt(c.copySource, row)
	p.CopyProgress = strAt(c.copyProgress, row)
	p.CopyCompletionTime = c.copyCompletionTime.at(row)
	p.CopyStatusDescription = strAt(c.copyStatusDescription, row)
	p.DestinationSnapshot = strAt(c.destinationSnapshot, row)
	p.ImmutabilityPolicyExpiresOn = c.immutabilityPolicyExpiry.at(row)
	p.ImmutabilityPolicyMode = enumAt[generated.ImmutabilityPolicyMode](c.immutabilityPolicyMode, row)
	p.LegalHold = boolAt(c.legalHold, row)
	p.DeletedTime = c.deletedTime.at(row)
	p.RemainingRetentionDays = c.remainingRetentionDays.at(row)
	p.LastAccessedOn = c.lastAccessedOn.at(row)
	p.TagCount = c.tagCount.at(row)
	p.BlobSequenceNumber = c.blobSequenceNumber.at(row)
	p.ResourceType = strAt(c.resourceType, row)
}

// ---------------------------------------------------------------------------
// Per-row accessors — no map lookup or type assertion, just nil-check + value.
// ---------------------------------------------------------------------------

func strAt(a *array.String, row int) *string {
	if a == nil || a.IsNull(row) {
		return nil
	}
	v := a.Value(row)
	return &v
}

func boolAt(a *array.Boolean, row int) *bool {
	if a == nil || a.IsNull(row) {
		return nil
	}
	v := a.Value(row)
	return &v
}

func etagAt(a *array.String, row int) *azcore.ETag {
	s := strAt(a, row)
	if s == nil {
		return nil
	}
	e := azcore.ETag(*s)
	return &e
}

func base64At(a *array.String, row int) []byte {
	s := strAt(a, row)
	if s == nil {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(*s)
	if err != nil {
		return nil
	}
	return decoded
}

type stringEnum interface {
	~string
}

func enumAt[T stringEnum](a *array.String, row int) *T {
	s := strAt(a, row)
	if s == nil {
		return nil
	}
	v := T(*s)
	return &v
}
