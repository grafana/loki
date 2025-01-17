// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"net/url"
	"os"
	"sync"

	"cloud.google.com/go/iam/apiv1/iampb"
	"cloud.google.com/go/internal/trace"
	gapic "cloud.google.com/go/storage/internal/apiv2"
	"cloud.google.com/go/storage/internal/apiv2/storagepb"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	"google.golang.org/api/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	fieldmaskpb "google.golang.org/protobuf/types/known/fieldmaskpb"
)

const (
	// defaultConnPoolSize is the default number of channels
	// to initialize in the GAPIC gRPC connection pool. A larger
	// connection pool may be necessary for jobs that require
	// high throughput and/or leverage many concurrent streams
	// if not running via DirectPath.
	//
	// This is only used for the gRPC client.
	defaultConnPoolSize = 1

	// maxPerMessageWriteSize is the maximum amount of content that can be sent
	// per WriteObjectRequest message. A buffer reaching this amount will
	// precipitate a flush of the buffer. It is only used by the gRPC Writer
	// implementation.
	maxPerMessageWriteSize int = int(storagepb.ServiceConstants_MAX_WRITE_CHUNK_BYTES)

	// globalProjectAlias is the project ID alias used for global buckets.
	//
	// This is only used for the gRPC API.
	globalProjectAlias = "_"

	// msgEntityNotSupported indicates ACL entites using project ID are not currently supported.
	//
	// This is only used for the gRPC API.
	msgEntityNotSupported = "The gRPC API currently does not support ACL entities using project ID, use project numbers instead"
)

// defaultGRPCOptions returns a set of the default client options
// for gRPC client initialization.
func defaultGRPCOptions() []option.ClientOption {
	defaults := []option.ClientOption{
		option.WithGRPCConnectionPool(defaultConnPoolSize),
	}

	// Set emulator options for gRPC if an emulator was specified. Note that in a
	// hybrid client, STORAGE_EMULATOR_HOST will set the host to use for HTTP and
	// STORAGE_EMULATOR_HOST_GRPC will set the host to use for gRPC (when using a
	// local emulator, HTTP and gRPC must use different ports, so this is
	// necessary).
	//
	// TODO: When the newHybridClient is not longer used, remove
	// STORAGE_EMULATOR_HOST_GRPC and use STORAGE_EMULATOR_HOST for both the
	// HTTP and gRPC based clients.
	if host := os.Getenv("STORAGE_EMULATOR_HOST_GRPC"); host != "" {
		// Strip the scheme from the emulator host. WithEndpoint does not take a
		// scheme for gRPC.
		host = stripScheme(host)

		defaults = append(defaults,
			option.WithEndpoint(host),
			option.WithGRPCDialOption(grpc.WithInsecure()),
			option.WithoutAuthentication(),
			WithDisabledClientMetrics(),
		)
	} else {
		// Only enable DirectPath when the emulator is not being targeted.
		defaults = append(defaults,
			internaloption.EnableDirectPath(true),
			internaloption.EnableDirectPathXds())
	}

	return defaults
}

// grpcStorageClient is the gRPC API implementation of the transport-agnostic
// storageClient interface.
type grpcStorageClient struct {
	raw      *gapic.Client
	settings *settings
	config   *storageConfig
}

func enableClientMetrics(ctx context.Context, s *settings, config storageConfig) (*metricsContext, error) {
	var project string
	// TODO: use new auth client
	c, err := transport.Creds(ctx, s.clientOption...)
	if err == nil {
		project = c.ProjectID
	}
	metricsContext, err := newGRPCMetricContext(ctx, metricsConfig{
		project:      project,
		interval:     config.metricInterval,
		manualReader: config.manualReader},
	)
	if err != nil {
		return nil, fmt.Errorf("gRPC Metrics: %w", err)
	}
	return metricsContext, nil
}

// newGRPCStorageClient initializes a new storageClient that uses the gRPC
// Storage API.
func newGRPCStorageClient(ctx context.Context, opts ...storageOption) (storageClient, error) {
	s := initSettings(opts...)
	s.clientOption = append(defaultGRPCOptions(), s.clientOption...)
	// Disable all gax-level retries in favor of retry logic in the veneer client.
	s.gax = append(s.gax, gax.WithRetry(nil))

	config := newStorageConfig(s.clientOption...)
	if config.readAPIWasSet {
		return nil, errors.New("storage: GRPC is incompatible with any option that specifies an API for reads")
	}

	if !config.disableClientMetrics {
		// Do not fail client creation if enabling metrics fails.
		if metricsContext, err := enableClientMetrics(ctx, s, config); err == nil {
			s.metricsContext = metricsContext
			s.clientOption = append(s.clientOption, metricsContext.clientOpts...)
		} else {
			log.Printf("Failed to enable client metrics: %v", err)
		}
	}
	g, err := gapic.NewClient(ctx, s.clientOption...)
	if err != nil {
		return nil, err
	}

	return &grpcStorageClient{
		raw:      g,
		settings: s,
		config:   &config,
	}, nil
}

func (c *grpcStorageClient) Close() error {
	if c.settings.metricsContext != nil {
		c.settings.metricsContext.close()
	}
	return c.raw.Close()
}

// Top-level methods.

// GetServiceAccount is not supported in the gRPC client.
func (c *grpcStorageClient) GetServiceAccount(ctx context.Context, project string, opts ...storageOption) (string, error) {
	return "", errMethodNotSupported
}

func (c *grpcStorageClient) CreateBucket(ctx context.Context, project, bucket string, attrs *BucketAttrs, enableObjectRetention *bool, opts ...storageOption) (*BucketAttrs, error) {
	if enableObjectRetention != nil {
		// TO-DO: implement ObjectRetention once available - see b/308194853
		return nil, status.Errorf(codes.Unimplemented, "storage: object retention is not supported in gRPC")
	}

	s := callSettings(c.settings, opts...)
	b := attrs.toProtoBucket()
	b.Project = toProjectResource(project)
	// If there is lifecycle information but no location, explicitly set
	// the location. This is a GCS quirk/bug.
	if b.GetLocation() == "" && b.GetLifecycle() != nil {
		b.Location = "US"
	}

	req := &storagepb.CreateBucketRequest{
		Parent:   fmt.Sprintf("projects/%s", globalProjectAlias),
		Bucket:   b,
		BucketId: bucket,
	}
	if attrs != nil {
		req.PredefinedAcl = attrs.PredefinedACL
		req.PredefinedDefaultObjectAcl = attrs.PredefinedDefaultObjectACL
	}

	var battrs *BucketAttrs
	err := run(ctx, func(ctx context.Context) error {
		res, err := c.raw.CreateBucket(ctx, req, s.gax...)

		battrs = newBucketFromProto(res)

		return err
	}, s.retry, s.idempotent)

	return battrs, err
}

func (c *grpcStorageClient) ListBuckets(ctx context.Context, project string, opts ...storageOption) *BucketIterator {
	s := callSettings(c.settings, opts...)
	it := &BucketIterator{
		ctx:       ctx,
		projectID: project,
	}

	var gitr *gapic.BucketIterator
	fetch := func(pageSize int, pageToken string) (token string, err error) {

		var buckets []*storagepb.Bucket
		var next string
		err = run(it.ctx, func(ctx context.Context) error {
			// Initialize GAPIC-based iterator when pageToken is empty, which
			// indicates that this fetch call is attempting to get the first page.
			//
			// Note: Initializing the GAPIC-based iterator lazily is necessary to
			// capture the BucketIterator.Prefix set by the user *after* the
			// BucketIterator is returned to them from the veneer.
			if pageToken == "" {
				req := &storagepb.ListBucketsRequest{
					Parent: toProjectResource(it.projectID),
					Prefix: it.Prefix,
				}
				gitr = c.raw.ListBuckets(ctx, req, s.gax...)
			}
			buckets, next, err = gitr.InternalFetch(pageSize, pageToken)
			return err
		}, s.retry, s.idempotent)
		if err != nil {
			return "", err
		}

		for _, bkt := range buckets {
			b := newBucketFromProto(bkt)
			it.buckets = append(it.buckets, b)
		}

		return next, nil
	}
	it.pageInfo, it.nextFunc = iterator.NewPageInfo(
		fetch,
		func() int { return len(it.buckets) },
		func() interface{} { b := it.buckets; it.buckets = nil; return b })

	return it
}

// Bucket methods.

func (c *grpcStorageClient) DeleteBucket(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) error {
	s := callSettings(c.settings, opts...)
	req := &storagepb.DeleteBucketRequest{
		Name: bucketResourceName(globalProjectAlias, bucket),
	}
	if err := applyBucketCondsProto("grpcStorageClient.DeleteBucket", conds, req); err != nil {
		return err
	}
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	return run(ctx, func(ctx context.Context) error {
		return c.raw.DeleteBucket(ctx, req, s.gax...)
	}, s.retry, s.idempotent)
}

func (c *grpcStorageClient) GetBucket(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) (*BucketAttrs, error) {
	s := callSettings(c.settings, opts...)
	req := &storagepb.GetBucketRequest{
		Name:     bucketResourceName(globalProjectAlias, bucket),
		ReadMask: &fieldmaskpb.FieldMask{Paths: []string{"*"}},
	}
	if err := applyBucketCondsProto("grpcStorageClient.GetBucket", conds, req); err != nil {
		return nil, err
	}
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	var battrs *BucketAttrs
	err := run(ctx, func(ctx context.Context) error {
		res, err := c.raw.GetBucket(ctx, req, s.gax...)

		battrs = newBucketFromProto(res)

		return err
	}, s.retry, s.idempotent)

	if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
		return nil, ErrBucketNotExist
	}

	return battrs, err
}
func (c *grpcStorageClient) UpdateBucket(ctx context.Context, bucket string, uattrs *BucketAttrsToUpdate, conds *BucketConditions, opts ...storageOption) (*BucketAttrs, error) {
	s := callSettings(c.settings, opts...)
	b := uattrs.toProtoBucket()
	b.Name = bucketResourceName(globalProjectAlias, bucket)
	req := &storagepb.UpdateBucketRequest{
		Bucket:                     b,
		PredefinedAcl:              uattrs.PredefinedACL,
		PredefinedDefaultObjectAcl: uattrs.PredefinedDefaultObjectACL,
	}
	if err := applyBucketCondsProto("grpcStorageClient.UpdateBucket", conds, req); err != nil {
		return nil, err
	}
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	var paths []string
	fieldMask := &fieldmaskpb.FieldMask{
		Paths: paths,
	}
	if uattrs.CORS != nil {
		fieldMask.Paths = append(fieldMask.Paths, "cors")
	}
	if uattrs.DefaultEventBasedHold != nil {
		fieldMask.Paths = append(fieldMask.Paths, "default_event_based_hold")
	}
	if uattrs.RetentionPolicy != nil {
		fieldMask.Paths = append(fieldMask.Paths, "retention_policy")
	}
	if uattrs.VersioningEnabled != nil {
		fieldMask.Paths = append(fieldMask.Paths, "versioning")
	}
	if uattrs.RequesterPays != nil {
		fieldMask.Paths = append(fieldMask.Paths, "billing")
	}
	if uattrs.BucketPolicyOnly != nil || uattrs.UniformBucketLevelAccess != nil || uattrs.PublicAccessPrevention != PublicAccessPreventionUnknown {
		fieldMask.Paths = append(fieldMask.Paths, "iam_config")
	}
	if uattrs.Encryption != nil {
		fieldMask.Paths = append(fieldMask.Paths, "encryption")
	}
	if uattrs.Lifecycle != nil {
		fieldMask.Paths = append(fieldMask.Paths, "lifecycle")
	}
	if uattrs.Logging != nil {
		fieldMask.Paths = append(fieldMask.Paths, "logging")
	}
	if uattrs.Website != nil {
		fieldMask.Paths = append(fieldMask.Paths, "website")
	}
	if uattrs.PredefinedACL != "" {
		// In cases where PredefinedACL is set, Acl is cleared.
		fieldMask.Paths = append(fieldMask.Paths, "acl")
	}
	if uattrs.PredefinedDefaultObjectACL != "" {
		// In cases where PredefinedDefaultObjectACL is set, DefaultObjectAcl is cleared.
		fieldMask.Paths = append(fieldMask.Paths, "default_object_acl")
	}
	// Note: This API currently does not support entites using project ID.
	// Use project numbers in ACL entities. Pending b/233617896.
	if uattrs.acl != nil {
		// In cases where acl is set by UpdateBucketACL method.
		fieldMask.Paths = append(fieldMask.Paths, "acl")
	}
	if uattrs.defaultObjectACL != nil {
		// In cases where defaultObjectACL is set by UpdateBucketACL method.
		fieldMask.Paths = append(fieldMask.Paths, "default_object_acl")
	}
	if uattrs.StorageClass != "" {
		fieldMask.Paths = append(fieldMask.Paths, "storage_class")
	}
	if uattrs.RPO != RPOUnknown {
		fieldMask.Paths = append(fieldMask.Paths, "rpo")
	}
	if uattrs.Autoclass != nil {
		fieldMask.Paths = append(fieldMask.Paths, "autoclass")
	}
	if uattrs.SoftDeletePolicy != nil {
		fieldMask.Paths = append(fieldMask.Paths, "soft_delete_policy")
	}

	for label := range uattrs.setLabels {
		fieldMask.Paths = append(fieldMask.Paths, fmt.Sprintf("labels.%s", label))
	}

	// Delete a label by not including it in Bucket.Labels but adding the key to the update mask.
	for label := range uattrs.deleteLabels {
		fieldMask.Paths = append(fieldMask.Paths, fmt.Sprintf("labels.%s", label))
	}

	req.UpdateMask = fieldMask

	if len(fieldMask.Paths) < 1 {
		// Nothing to update. Send a get request for current attrs instead. This
		// maintains consistency with JSON bucket updates.
		opts = append(opts, idempotent(true))
		return c.GetBucket(ctx, bucket, conds, opts...)
	}

	var battrs *BucketAttrs
	err := run(ctx, func(ctx context.Context) error {
		res, err := c.raw.UpdateBucket(ctx, req, s.gax...)
		battrs = newBucketFromProto(res)
		return err
	}, s.retry, s.idempotent)

	return battrs, err
}
func (c *grpcStorageClient) LockBucketRetentionPolicy(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) error {
	s := callSettings(c.settings, opts...)
	req := &storagepb.LockBucketRetentionPolicyRequest{
		Bucket: bucketResourceName(globalProjectAlias, bucket),
	}
	if err := applyBucketCondsProto("grpcStorageClient.LockBucketRetentionPolicy", conds, req); err != nil {
		return err
	}

	return run(ctx, func(ctx context.Context) error {
		_, err := c.raw.LockBucketRetentionPolicy(ctx, req, s.gax...)
		return err
	}, s.retry, s.idempotent)

}
func (c *grpcStorageClient) ListObjects(ctx context.Context, bucket string, q *Query, opts ...storageOption) *ObjectIterator {
	s := callSettings(c.settings, opts...)
	it := &ObjectIterator{
		ctx: ctx,
	}
	if q != nil {
		it.query = *q
	}
	req := &storagepb.ListObjectsRequest{
		Parent:                   bucketResourceName(globalProjectAlias, bucket),
		Prefix:                   it.query.Prefix,
		Delimiter:                it.query.Delimiter,
		Versions:                 it.query.Versions,
		LexicographicStart:       it.query.StartOffset,
		LexicographicEnd:         it.query.EndOffset,
		IncludeTrailingDelimiter: it.query.IncludeTrailingDelimiter,
		MatchGlob:                it.query.MatchGlob,
		ReadMask:                 q.toFieldMask(), // a nil Query still results in a "*" FieldMask
		SoftDeleted:              it.query.SoftDeleted,
		IncludeFoldersAsPrefixes: it.query.IncludeFoldersAsPrefixes,
	}
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}
	fetch := func(pageSize int, pageToken string) (token string, err error) {
		var objects []*storagepb.Object
		var gitr *gapic.ObjectIterator
		err = run(it.ctx, func(ctx context.Context) error {
			gitr = c.raw.ListObjects(ctx, req, s.gax...)
			it.ctx = ctx
			objects, token, err = gitr.InternalFetch(pageSize, pageToken)
			return err
		}, s.retry, s.idempotent)
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				err = ErrBucketNotExist
			}
			return "", err
		}

		for _, obj := range objects {
			b := newObjectFromProto(obj)
			it.items = append(it.items, b)
		}

		// Response is always non-nil after a successful request.
		res := gitr.Response.(*storagepb.ListObjectsResponse)
		for _, prefix := range res.GetPrefixes() {
			it.items = append(it.items, &ObjectAttrs{Prefix: prefix})
		}

		return token, nil
	}
	it.pageInfo, it.nextFunc = iterator.NewPageInfo(
		fetch,
		func() int { return len(it.items) },
		func() interface{} { b := it.items; it.items = nil; return b })

	return it
}

// Object metadata methods.

func (c *grpcStorageClient) DeleteObject(ctx context.Context, bucket, object string, gen int64, conds *Conditions, opts ...storageOption) error {
	s := callSettings(c.settings, opts...)
	req := &storagepb.DeleteObjectRequest{
		Bucket: bucketResourceName(globalProjectAlias, bucket),
		Object: object,
	}
	if err := applyCondsProto("grpcStorageClient.DeleteObject", gen, conds, req); err != nil {
		return err
	}
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}
	err := run(ctx, func(ctx context.Context) error {
		return c.raw.DeleteObject(ctx, req, s.gax...)
	}, s.retry, s.idempotent)
	if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
		return ErrObjectNotExist
	}
	return err
}

func (c *grpcStorageClient) GetObject(ctx context.Context, params *getObjectParams, opts ...storageOption) (*ObjectAttrs, error) {
	s := callSettings(c.settings, opts...)
	req := &storagepb.GetObjectRequest{
		Bucket: bucketResourceName(globalProjectAlias, params.bucket),
		Object: params.object,
		// ProjectionFull by default.
		ReadMask: &fieldmaskpb.FieldMask{Paths: []string{"*"}},
	}
	if err := applyCondsProto("grpcStorageClient.GetObject", params.gen, params.conds, req); err != nil {
		return nil, err
	}
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}
	if params.encryptionKey != nil {
		req.CommonObjectRequestParams = toProtoCommonObjectRequestParams(params.encryptionKey)
	}
	if params.softDeleted {
		req.SoftDeleted = &params.softDeleted
	}

	var attrs *ObjectAttrs
	err := run(ctx, func(ctx context.Context) error {
		res, err := c.raw.GetObject(ctx, req, s.gax...)
		attrs = newObjectFromProto(res)

		return err
	}, s.retry, s.idempotent)

	if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
		return nil, ErrObjectNotExist
	}

	return attrs, err
}

func (c *grpcStorageClient) UpdateObject(ctx context.Context, params *updateObjectParams, opts ...storageOption) (*ObjectAttrs, error) {
	uattrs := params.uattrs
	if params.overrideRetention != nil || uattrs.Retention != nil {
		// TO-DO: implement ObjectRetention once available - see b/308194853
		return nil, status.Errorf(codes.Unimplemented, "storage: object retention is not supported in gRPC")
	}
	s := callSettings(c.settings, opts...)
	o := uattrs.toProtoObject(bucketResourceName(globalProjectAlias, params.bucket), params.object)
	// For Update, generation is passed via the object message rather than a field on the request.
	if params.gen >= 0 {
		o.Generation = params.gen
	}
	req := &storagepb.UpdateObjectRequest{
		Object:        o,
		PredefinedAcl: uattrs.PredefinedACL,
	}
	if err := applyCondsProto("grpcStorageClient.UpdateObject", defaultGen, params.conds, req); err != nil {
		return nil, err
	}
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}
	if params.encryptionKey != nil {
		req.CommonObjectRequestParams = toProtoCommonObjectRequestParams(params.encryptionKey)
	}

	fieldMask := &fieldmaskpb.FieldMask{Paths: nil}
	if uattrs.EventBasedHold != nil {
		fieldMask.Paths = append(fieldMask.Paths, "event_based_hold")
	}
	if uattrs.TemporaryHold != nil {
		fieldMask.Paths = append(fieldMask.Paths, "temporary_hold")
	}
	if uattrs.ContentType != nil {
		fieldMask.Paths = append(fieldMask.Paths, "content_type")
	}
	if uattrs.ContentLanguage != nil {
		fieldMask.Paths = append(fieldMask.Paths, "content_language")
	}
	if uattrs.ContentEncoding != nil {
		fieldMask.Paths = append(fieldMask.Paths, "content_encoding")
	}
	if uattrs.ContentDisposition != nil {
		fieldMask.Paths = append(fieldMask.Paths, "content_disposition")
	}
	if uattrs.CacheControl != nil {
		fieldMask.Paths = append(fieldMask.Paths, "cache_control")
	}
	if !uattrs.CustomTime.IsZero() {
		fieldMask.Paths = append(fieldMask.Paths, "custom_time")
	}
	// Note: This API currently does not support entites using project ID.
	// Use project numbers in ACL entities. Pending b/233617896.
	if uattrs.ACL != nil || len(uattrs.PredefinedACL) > 0 {
		fieldMask.Paths = append(fieldMask.Paths, "acl")
	}

	if uattrs.Metadata != nil {
		// We don't support deleting a specific metadata key; metadata is deleted
		// as a whole if provided an empty map, so we do not use dot notation here
		if len(uattrs.Metadata) == 0 {
			fieldMask.Paths = append(fieldMask.Paths, "metadata")
		} else {
			// We can, however, use dot notation for adding keys
			for key := range uattrs.Metadata {
				fieldMask.Paths = append(fieldMask.Paths, fmt.Sprintf("metadata.%s", key))
			}
		}
	}

	req.UpdateMask = fieldMask

	if len(fieldMask.Paths) < 1 {
		// Nothing to update. To maintain consistency with JSON, we must still
		// update the object because metageneration and other fields are
		// updated even on an empty update.
		// gRPC will fail if the fieldmask is empty, so instead we add an
		// output-only field to the update mask. Output-only fields are (and must
		// be - see AIP 161) ignored, but allow us to send an empty update because
		// any mask that is valid for read (as this one is) must be valid for write.
		fieldMask.Paths = append(fieldMask.Paths, "create_time")
	}

	var attrs *ObjectAttrs
	err := run(ctx, func(ctx context.Context) error {
		res, err := c.raw.UpdateObject(ctx, req, s.gax...)
		attrs = newObjectFromProto(res)
		return err
	}, s.retry, s.idempotent)
	if e, ok := status.FromError(err); ok && e.Code() == codes.NotFound {
		return nil, ErrObjectNotExist
	}

	return attrs, err
}

func (c *grpcStorageClient) RestoreObject(ctx context.Context, params *restoreObjectParams, opts ...storageOption) (*ObjectAttrs, error) {
	s := callSettings(c.settings, opts...)
	req := &storagepb.RestoreObjectRequest{
		Bucket:        bucketResourceName(globalProjectAlias, params.bucket),
		Object:        params.object,
		CopySourceAcl: &params.copySourceACL,
	}
	if err := applyCondsProto("grpcStorageClient.RestoreObject", params.gen, params.conds, req); err != nil {
		return nil, err
	}
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	var attrs *ObjectAttrs
	err := run(ctx, func(ctx context.Context) error {
		res, err := c.raw.RestoreObject(ctx, req, s.gax...)
		attrs = newObjectFromProto(res)
		return err
	}, s.retry, s.idempotent)
	if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
		return nil, ErrObjectNotExist
	}
	return attrs, err
}

func (c *grpcStorageClient) MoveObject(ctx context.Context, params *moveObjectParams, opts ...storageOption) (*ObjectAttrs, error) {
	s := callSettings(c.settings, opts...)
	req := &storagepb.MoveObjectRequest{
		Bucket:            bucketResourceName(globalProjectAlias, params.bucket),
		SourceObject:      params.srcObject,
		DestinationObject: params.dstObject,
	}
	if err := applyCondsProto("MoveObjectDestination", defaultGen, params.dstConds, req); err != nil {
		return nil, err
	}
	if err := applySourceCondsProto("MoveObjectSource", defaultGen, params.srcConds, req); err != nil {
		return nil, err
	}

	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	var attrs *ObjectAttrs
	err := run(ctx, func(ctx context.Context) error {
		res, err := c.raw.MoveObject(ctx, req, s.gax...)
		attrs = newObjectFromProto(res)
		return err
	}, s.retry, s.idempotent)
	if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
		return nil, ErrObjectNotExist
	}
	return attrs, err
}

// Default Object ACL methods.

func (c *grpcStorageClient) DeleteDefaultObjectACL(ctx context.Context, bucket string, entity ACLEntity, opts ...storageOption) error {
	// There is no separate API for PATCH in gRPC.
	// Make a GET call first to retrieve BucketAttrs.
	attrs, err := c.GetBucket(ctx, bucket, nil, opts...)
	if err != nil {
		return err
	}
	// Delete the entity and copy other remaining ACL entities.
	// Note: This API currently does not support entites using project ID.
	// Use project numbers in ACL entities. Pending b/233617896.
	// Return error if entity is not found or a project ID is used.
	invalidEntity := true
	var acl []ACLRule
	for _, a := range attrs.DefaultObjectACL {
		if a.Entity != entity {
			acl = append(acl, a)
		}
		if a.Entity == entity {
			invalidEntity = false
		}
	}
	if invalidEntity {
		return fmt.Errorf("storage: entity %v was not found on bucket %v, got %v. %v", entity, bucket, attrs.DefaultObjectACL, msgEntityNotSupported)
	}
	uattrs := &BucketAttrsToUpdate{defaultObjectACL: acl}
	// Call UpdateBucket with a MetagenerationMatch precondition set.
	if _, err = c.UpdateBucket(ctx, bucket, uattrs, &BucketConditions{MetagenerationMatch: attrs.MetaGeneration}, opts...); err != nil {
		return err
	}
	return nil
}

func (c *grpcStorageClient) ListDefaultObjectACLs(ctx context.Context, bucket string, opts ...storageOption) ([]ACLRule, error) {
	attrs, err := c.GetBucket(ctx, bucket, nil, opts...)
	if err != nil {
		return nil, err
	}
	return attrs.DefaultObjectACL, nil
}

func (c *grpcStorageClient) UpdateDefaultObjectACL(ctx context.Context, bucket string, entity ACLEntity, role ACLRole, opts ...storageOption) error {
	// There is no separate API for PATCH in gRPC.
	// Make a GET call first to retrieve BucketAttrs.
	attrs, err := c.GetBucket(ctx, bucket, nil, opts...)
	if err != nil {
		return err
	}
	// Note: This API currently does not support entites using project ID.
	// Use project numbers in ACL entities. Pending b/233617896.
	var acl []ACLRule
	aclRule := ACLRule{Entity: entity, Role: role}
	acl = append(attrs.DefaultObjectACL, aclRule)
	uattrs := &BucketAttrsToUpdate{defaultObjectACL: acl}
	// Call UpdateBucket with a MetagenerationMatch precondition set.
	if _, err = c.UpdateBucket(ctx, bucket, uattrs, &BucketConditions{MetagenerationMatch: attrs.MetaGeneration}, opts...); err != nil {
		return err
	}
	return nil
}

// Bucket ACL methods.

func (c *grpcStorageClient) DeleteBucketACL(ctx context.Context, bucket string, entity ACLEntity, opts ...storageOption) error {
	// There is no separate API for PATCH in gRPC.
	// Make a GET call first to retrieve BucketAttrs.
	attrs, err := c.GetBucket(ctx, bucket, nil, opts...)
	if err != nil {
		return err
	}
	// Delete the entity and copy other remaining ACL entities.
	// Note: This API currently does not support entites using project ID.
	// Use project numbers in ACL entities. Pending b/233617896.
	// Return error if entity is not found or a project ID is used.
	invalidEntity := true
	var acl []ACLRule
	for _, a := range attrs.ACL {
		if a.Entity != entity {
			acl = append(acl, a)
		}
		if a.Entity == entity {
			invalidEntity = false
		}
	}
	if invalidEntity {
		return fmt.Errorf("storage: entity %v was not found on bucket %v, got %v. %v", entity, bucket, attrs.ACL, msgEntityNotSupported)
	}
	uattrs := &BucketAttrsToUpdate{acl: acl}
	// Call UpdateBucket with a MetagenerationMatch precondition set.
	if _, err = c.UpdateBucket(ctx, bucket, uattrs, &BucketConditions{MetagenerationMatch: attrs.MetaGeneration}, opts...); err != nil {
		return err
	}
	return nil
}

func (c *grpcStorageClient) ListBucketACLs(ctx context.Context, bucket string, opts ...storageOption) ([]ACLRule, error) {
	attrs, err := c.GetBucket(ctx, bucket, nil, opts...)
	if err != nil {
		return nil, err
	}
	return attrs.ACL, nil
}

func (c *grpcStorageClient) UpdateBucketACL(ctx context.Context, bucket string, entity ACLEntity, role ACLRole, opts ...storageOption) error {
	// There is no separate API for PATCH in gRPC.
	// Make a GET call first to retrieve BucketAttrs.
	attrs, err := c.GetBucket(ctx, bucket, nil, opts...)
	if err != nil {
		return err
	}
	// Note: This API currently does not support entites using project ID.
	// Use project numbers in ACL entities. Pending b/233617896.
	var acl []ACLRule
	aclRule := ACLRule{Entity: entity, Role: role}
	acl = append(attrs.ACL, aclRule)
	uattrs := &BucketAttrsToUpdate{acl: acl}
	// Call UpdateBucket with a MetagenerationMatch precondition set.
	if _, err = c.UpdateBucket(ctx, bucket, uattrs, &BucketConditions{MetagenerationMatch: attrs.MetaGeneration}, opts...); err != nil {
		return err
	}
	return nil
}

// Object ACL methods.

func (c *grpcStorageClient) DeleteObjectACL(ctx context.Context, bucket, object string, entity ACLEntity, opts ...storageOption) error {
	// There is no separate API for PATCH in gRPC.
	// Make a GET call first to retrieve ObjectAttrs.
	attrs, err := c.GetObject(ctx, &getObjectParams{bucket, object, defaultGen, nil, nil, false}, opts...)
	if err != nil {
		return err
	}
	// Delete the entity and copy other remaining ACL entities.
	// Note: This API currently does not support entites using project ID.
	// Use project numbers in ACL entities. Pending b/233617896.
	// Return error if entity is not found or a project ID is used.
	invalidEntity := true
	var acl []ACLRule
	for _, a := range attrs.ACL {
		if a.Entity != entity {
			acl = append(acl, a)
		}
		if a.Entity == entity {
			invalidEntity = false
		}
	}
	if invalidEntity {
		return fmt.Errorf("storage: entity %v was not found on bucket %v, got %v. %v", entity, bucket, attrs.ACL, msgEntityNotSupported)
	}
	uattrs := &ObjectAttrsToUpdate{ACL: acl}
	// Call UpdateObject with the specified metageneration.
	params := &updateObjectParams{bucket: bucket, object: object, uattrs: uattrs, gen: defaultGen, conds: &Conditions{MetagenerationMatch: attrs.Metageneration}}
	if _, err = c.UpdateObject(ctx, params, opts...); err != nil {
		return err
	}
	return nil
}

// ListObjectACLs retrieves object ACL entries. By default, it operates on the latest generation of this object.
// Selecting a specific generation of this object is not currently supported by the client.
func (c *grpcStorageClient) ListObjectACLs(ctx context.Context, bucket, object string, opts ...storageOption) ([]ACLRule, error) {
	o, err := c.GetObject(ctx, &getObjectParams{bucket, object, defaultGen, nil, nil, false}, opts...)
	if err != nil {
		return nil, err
	}
	return o.ACL, nil
}

func (c *grpcStorageClient) UpdateObjectACL(ctx context.Context, bucket, object string, entity ACLEntity, role ACLRole, opts ...storageOption) error {
	// There is no separate API for PATCH in gRPC.
	// Make a GET call first to retrieve ObjectAttrs.
	attrs, err := c.GetObject(ctx, &getObjectParams{bucket, object, defaultGen, nil, nil, false}, opts...)
	if err != nil {
		return err
	}
	// Note: This API currently does not support entites using project ID.
	// Use project numbers in ACL entities. Pending b/233617896.
	var acl []ACLRule
	aclRule := ACLRule{Entity: entity, Role: role}
	acl = append(attrs.ACL, aclRule)
	uattrs := &ObjectAttrsToUpdate{ACL: acl}
	// Call UpdateObject with the specified metageneration.
	params := &updateObjectParams{bucket: bucket, object: object, uattrs: uattrs, gen: defaultGen, conds: &Conditions{MetagenerationMatch: attrs.Metageneration}}
	if _, err = c.UpdateObject(ctx, params, opts...); err != nil {
		return err
	}
	return nil
}

// Media operations.

func (c *grpcStorageClient) ComposeObject(ctx context.Context, req *composeObjectRequest, opts ...storageOption) (*ObjectAttrs, error) {
	s := callSettings(c.settings, opts...)
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	dstObjPb := req.dstObject.attrs.toProtoObject(req.dstBucket)
	dstObjPb.Name = req.dstObject.name

	if req.sendCRC32C {
		dstObjPb.Checksums.Crc32C = &req.dstObject.attrs.CRC32C
	}

	srcs := []*storagepb.ComposeObjectRequest_SourceObject{}
	for _, src := range req.srcs {
		srcObjPb := &storagepb.ComposeObjectRequest_SourceObject{Name: src.name, ObjectPreconditions: &storagepb.ComposeObjectRequest_SourceObject_ObjectPreconditions{}}
		if src.gen >= 0 {
			srcObjPb.Generation = src.gen
		}
		if err := applyCondsProto("ComposeObject source", defaultGen, src.conds, srcObjPb.ObjectPreconditions); err != nil {
			return nil, err
		}
		srcs = append(srcs, srcObjPb)
	}

	rawReq := &storagepb.ComposeObjectRequest{
		Destination:   dstObjPb,
		SourceObjects: srcs,
	}
	if err := applyCondsProto("ComposeObject destination", defaultGen, req.dstObject.conds, rawReq); err != nil {
		return nil, err
	}
	if req.predefinedACL != "" {
		rawReq.DestinationPredefinedAcl = req.predefinedACL
	}
	if req.dstObject.encryptionKey != nil {
		rawReq.CommonObjectRequestParams = toProtoCommonObjectRequestParams(req.dstObject.encryptionKey)
	}

	var obj *storagepb.Object
	var err error
	if err := run(ctx, func(ctx context.Context) error {
		obj, err = c.raw.ComposeObject(ctx, rawReq, s.gax...)
		return err
	}, s.retry, s.idempotent); err != nil {
		return nil, err
	}

	return newObjectFromProto(obj), nil
}
func (c *grpcStorageClient) RewriteObject(ctx context.Context, req *rewriteObjectRequest, opts ...storageOption) (*rewriteObjectResponse, error) {
	s := callSettings(c.settings, opts...)
	obj := req.dstObject.attrs.toProtoObject("")
	call := &storagepb.RewriteObjectRequest{
		SourceBucket:              bucketResourceName(globalProjectAlias, req.srcObject.bucket),
		SourceObject:              req.srcObject.name,
		RewriteToken:              req.token,
		DestinationBucket:         bucketResourceName(globalProjectAlias, req.dstObject.bucket),
		DestinationName:           req.dstObject.name,
		Destination:               obj,
		DestinationKmsKey:         req.dstObject.keyName,
		DestinationPredefinedAcl:  req.predefinedACL,
		CommonObjectRequestParams: toProtoCommonObjectRequestParams(req.dstObject.encryptionKey),
	}

	// The userProject, whether source or destination project, is decided by the code calling the interface.
	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}
	if err := applyCondsProto("Copy destination", defaultGen, req.dstObject.conds, call); err != nil {
		return nil, err
	}
	if err := applySourceCondsProto("Copy source", req.srcObject.gen, req.srcObject.conds, call); err != nil {
		return nil, err
	}

	if len(req.dstObject.encryptionKey) > 0 {
		call.CommonObjectRequestParams = toProtoCommonObjectRequestParams(req.dstObject.encryptionKey)
	}
	if len(req.srcObject.encryptionKey) > 0 {
		srcParams := toProtoCommonObjectRequestParams(req.srcObject.encryptionKey)
		call.CopySourceEncryptionAlgorithm = srcParams.GetEncryptionAlgorithm()
		call.CopySourceEncryptionKeyBytes = srcParams.GetEncryptionKeyBytes()
		call.CopySourceEncryptionKeySha256Bytes = srcParams.GetEncryptionKeySha256Bytes()
	}

	call.MaxBytesRewrittenPerCall = req.maxBytesRewrittenPerCall

	var res *storagepb.RewriteResponse
	var err error

	retryCall := func(ctx context.Context) error { res, err = c.raw.RewriteObject(ctx, call, s.gax...); return err }

	if err := run(ctx, retryCall, s.retry, s.idempotent); err != nil {
		return nil, err
	}

	r := &rewriteObjectResponse{
		done:     res.GetDone(),
		written:  res.GetTotalBytesRewritten(),
		size:     res.GetObjectSize(),
		token:    res.GetRewriteToken(),
		resource: newObjectFromProto(res.GetResource()),
	}

	return r, nil
}

// Custom codec to be used for unmarshaling BidiReadObjectResponse messages.
// This is used to avoid a copy of object data in proto.Unmarshal.
type bytesCodecV2 struct {
}

var _ encoding.CodecV2 = bytesCodecV2{}

// Marshal is used to encode messages to send for bytesCodecV2. Since we are only
// using this to send ReadObjectRequest messages we don't need to recycle buffers
// here.
func (bytesCodecV2) Marshal(v any) (mem.BufferSlice, error) {
	vv, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("failed to marshal, message is %T, want proto.Message", v)
	}
	var data mem.BufferSlice
	buf, err := proto.Marshal(vv)
	if err != nil {
		return nil, err
	}
	data = append(data, mem.SliceBuffer(buf))
	return data, nil
}

// Unmarshal is used for data received for BidiReadObjectResponse. We want to preserve
// the mem.BufferSlice in most cases rather than copying and calling proto.Unmarshal.
func (bytesCodecV2) Unmarshal(data mem.BufferSlice, v any) error {
	switch v := v.(type) {
	case *mem.BufferSlice:
		*v = data
		// Pick up a reference to the data so that it is not freed while decoding.
		data.Ref()
		return nil
	case proto.Message:
		buf := data.MaterializeToBuffer(mem.DefaultBufferPool())
		return proto.Unmarshal(buf.ReadOnlyData(), v)
	default:
		return fmt.Errorf("cannot unmarshal type %T, want proto.Message or mem.BufferSlice", v)
	}
}

func (bytesCodecV2) Name() string {
	return ""
}

func contextMetadataFromBidiReadObject(req *storagepb.BidiReadObjectRequest) []string {
	if len(req.GetReadObjectSpec().GetRoutingToken()) > 0 {
		return []string{"x-goog-request-params", fmt.Sprintf("bucket=%s&routing_token=%s", req.GetReadObjectSpec().GetBucket(), req.GetReadObjectSpec().GetRoutingToken())}
	}
	return []string{"x-goog-request-params", fmt.Sprintf("bucket=%s", req.GetReadObjectSpec().GetBucket())}
}

type rangeSpec struct {
	readID       int64
	writer       io.Writer
	offset       int64
	limit        int64
	bytesWritten int64
	callback     func(int64, int64, error)
}

func (c *grpcStorageClient) NewMultiRangeDownloader(ctx context.Context, params *newMultiRangeDownloaderParams, opts ...storageOption) (mr *MultiRangeDownloader, err error) {
	ctx = trace.StartSpan(ctx, "cloud.google.com/go/storage.grpcStorageClient.NewMultiRangeDownloader")
	defer func() { trace.EndSpan(ctx, err) }()
	s := callSettings(c.settings, opts...)

	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	b := bucketResourceName(globalProjectAlias, params.bucket)
	object := params.object
	r := &storagepb.BidiReadObjectSpec{
		Bucket:                    b,
		Object:                    object,
		CommonObjectRequestParams: toProtoCommonObjectRequestParams(params.encryptionKey),
	}

	// The default is a negative value, which means latest.
	if params.gen >= 0 {
		r.Generation = params.gen
	}

	if params.handle != nil {
		r.ReadHandle = &storagepb.BidiReadHandle{
			Handle: *params.handle,
		}
	}
	req := &storagepb.BidiReadObjectRequest{
		ReadObjectSpec: r,
	}

	ctx = gax.InsertMetadataIntoOutgoingContext(ctx, contextMetadataFromBidiReadObject(req)...)

	openStream := func() (*bidiReadStreamResponse, context.CancelFunc, error) {
		if err := applyCondsProto("grpcStorageClient.BidiReadObject", params.gen, params.conds, r); err != nil {
			return nil, nil, err
		}
		var stream storagepb.Storage_BidiReadObjectClient
		var resp *storagepb.BidiReadObjectResponse
		cc, cancel := context.WithCancel(ctx)
		err = run(cc, func(ctx context.Context) error {
			stream, err = c.raw.BidiReadObject(ctx, s.gax...)
			if err != nil {
				// BidiReadObjectRedirectedError error is only returned on initial open in case of a redirect.
				// The routing token that should be used when reopening the read stream. Needs to be exported.
				rpcStatus := status.Convert(err)
				details := rpcStatus.Details()
				for _, detail := range details {
					if bidiError, ok := detail.(*storagepb.BidiReadObjectRedirectedError); ok {
						r.ReadHandle = bidiError.ReadHandle
						r.RoutingToken = bidiError.RoutingToken
						req.ReadObjectSpec = r
						ctx = gax.InsertMetadataIntoOutgoingContext(ctx, contextMetadataFromBidiReadObject(req)...)
					}
				}
				return err
			}
			// Incase stream opened succesfully, send first message on the stream.
			// First message to stream should contain read_object_spec
			err = stream.Send(req)
			if err != nil {
				return err
			}
			resp, err = stream.Recv()
			if err != nil {
				return err
			}
			return nil
		}, s.retry, s.idempotent)
		if err != nil {
			// Close the stream context we just created to ensure we don't leak
			// resources.
			cancel()
			return nil, nil, err
		}
		return &bidiReadStreamResponse{stream: stream, response: resp}, cancel, nil
	}

	// For the first time open stream without adding any range.
	resp, cancel, err := openStream()
	if err != nil {
		return nil, err
	}

	// The first message was Recv'd on stream open, use it to populate the
	// object metadata.
	msg := resp.response
	obj := msg.GetMetadata()
	// This is the size of the entire object, even if only a range was requested.
	size := obj.GetSize()

	rr := &gRPCBidiReader{
		stream:           resp.stream,
		cancel:           cancel,
		settings:         s,
		readHandle:       msg.GetReadHandle().GetHandle(),
		readID:           1,
		reopen:           openStream,
		readSpec:         r,
		data:             make(chan []rangeSpec, 100),
		ctx:              ctx,
		closeReceiver:    make(chan bool, 10),
		closeManager:     make(chan bool, 10),
		managerRetry:     make(chan bool), // create unbuffered channel for closing the streamManager goroutine.
		receiverRetry:    make(chan bool), // create unbuffered channel for closing the streamReceiver goroutine.
		mp:               make(map[int64]rangeSpec),
		done:             false,
		activeTask:       0,
		streamRecreation: false,
	}

	// streamManager goroutine runs in background where we send message to gcs and process response.
	streamManager := func() {
		var currentSpec []rangeSpec
		for {
			select {
			case <-rr.ctx.Done():
				rr.mu.Lock()
				rr.done = true
				rr.mu.Unlock()
				return
			case <-rr.managerRetry:
				return
			case <-rr.closeManager:
				rr.mu.Lock()
				if len(rr.mp) != 0 {
					for key := range rr.mp {
						rr.mp[key].callback(rr.mp[key].offset, rr.mp[key].limit, fmt.Errorf("stream closed early"))
						delete(rr.mp, key)
					}
				}
				rr.mu.Unlock()
				return
			case currentSpec = <-rr.data:
				var readRanges []*storagepb.ReadRange
				var err error
				rr.mu.Lock()
				for _, v := range currentSpec {
					rr.mp[v.readID] = v
					readRanges = append(readRanges, &storagepb.ReadRange{ReadOffset: v.offset, ReadLength: v.limit, ReadId: v.readID})
				}
				rr.mu.Unlock()
				// We can just send 100 request to gcs in one request.
				// In case of Add we will send only one range request to gcs but in case of retry we can have more than 100 ranges.
				// Hence be will divide the request in chunk of 100.
				// For example with 457 ranges on stream we will have 5 request to gcs [0:99], [100:199], [200:299], [300:399], [400:456]
				requestCount := len(readRanges) / 100
				if len(readRanges)%100 != 0 {
					requestCount++
				}
				for i := 0; i < requestCount; i++ {
					start := i * 100
					end := (i + 1) * 100
					if end > len(readRanges) {
						end = len(readRanges)
					}
					curReq := readRanges[start:end]
					err = rr.stream.Send(&storagepb.BidiReadObjectRequest{
						ReadRanges: curReq,
					})
					if err != nil {
						// cancel stream and reopen the stream again.
						// Incase again an error is thrown close the streamManager goroutine.
						rr.retrier(err, "manager")
						break
					}
				}

			}
		}
	}

	streamReceiver := func() {
		var resp *storagepb.BidiReadObjectResponse
		var err error
		for {
			select {
			case <-rr.ctx.Done():
				rr.done = true
				return
			case <-rr.receiverRetry:
				return
			case <-rr.closeReceiver:
				return
			default:
				// This function reads the data sent for a particular range request and has a callback
				// to indicate that output buffer is filled.
				resp, err = rr.stream.Recv()
				if resp.GetReadHandle().GetHandle() != nil {
					rr.readHandle = resp.GetReadHandle().GetHandle()
				}
				if err == io.EOF {
					err = nil
				}
				if err != nil {
					// cancel stream and reopen the stream again.
					// Incase again an error is thrown close the streamManager goroutine.
					rr.retrier(err, "receiver")
				}

				if err == nil {
					rr.mu.Lock()
					if len(rr.mp) == 0 && rr.activeTask == 0 {
						rr.closeReceiver <- true
						rr.closeManager <- true
						return
					}
					rr.mu.Unlock()
					arr := resp.GetObjectDataRanges()
					for _, val := range arr {
						id := val.GetReadRange().GetReadId()
						rr.mu.Lock()
						_, err = rr.mp[id].writer.Write(val.GetChecksummedData().GetContent())
						if err != nil {
							rr.mp[id].callback(rr.mp[id].offset, rr.mp[id].limit, err)
							rr.activeTask--
							delete(rr.mp, id)
						} else {
							rr.mp[id] = rangeSpec{
								readID:       rr.mp[id].readID,
								writer:       rr.mp[id].writer,
								offset:       rr.mp[id].offset,
								limit:        rr.mp[id].limit,
								bytesWritten: rr.mp[id].bytesWritten + int64(len(val.GetChecksummedData().GetContent())),
								callback:     rr.mp[id].callback,
							}
						}
						if val.GetRangeEnd() {
							rr.mp[id].callback(rr.mp[id].offset, rr.mp[id].limit, nil)
							rr.activeTask--
							delete(rr.mp, id)
						}
						rr.mu.Unlock()
					}
				}

			}
		}
	}

	rr.retrier = func(err error, thread string) {
		rr.mu.Lock()
		if !rr.streamRecreation {
			rr.streamRecreation = true
		} else {
			rr.mu.Unlock()
			return
		}
		rr.mu.Unlock()
		// close both the go routines to make the stream recreation syncronous.
		if thread == "receiver" {
			rr.managerRetry <- true
		} else {
			rr.receiverRetry <- true
		}
		err = rr.retryStream(err)
		if err != nil {
			rr.mu.Lock()
			for key := range rr.mp {
				rr.mp[key].callback(rr.mp[key].offset, rr.mp[key].limit, err)
				delete(rr.mp, key)
			}
			rr.mu.Unlock()
			rr.close()
		} else {
			// If stream recreation happened successfully lets again start
			// both the goroutine making the whole flow asynchronous again.
			if thread == "receiver" {
				go streamManager()
			} else {
				go streamReceiver()
			}
		}
		rr.mu.Lock()
		rr.streamRecreation = false
		rr.mu.Unlock()
	}

	rr.mu.Lock()
	rr.objectSize = size
	rr.mu.Unlock()

	go streamManager()
	go streamReceiver()

	return &MultiRangeDownloader{
		Attrs: ReaderObjectAttrs{
			Size:            size,
			ContentType:     obj.GetContentType(),
			ContentEncoding: obj.GetContentEncoding(),
			CacheControl:    obj.GetCacheControl(),
			LastModified:    obj.GetUpdateTime().AsTime(),
			Metageneration:  obj.GetMetageneration(),
			Generation:      obj.GetGeneration(),
		},
		reader: rr,
	}, nil
}

func getActiveRange(r *gRPCBidiReader) []rangeSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	var activeRange []rangeSpec
	for k, v := range r.mp {
		activeRange = append(activeRange, rangeSpec{
			readID:       k,
			writer:       v.writer,
			offset:       (v.offset + v.bytesWritten),
			limit:        v.limit - v.bytesWritten,
			callback:     v.callback,
			bytesWritten: 0,
		})
		r.mp[k] = activeRange[len(activeRange)-1]
	}
	return activeRange
}

// retryStream cancel's stream and reopen the stream again.
func (r *gRPCBidiReader) retryStream(err error) error {
	var shouldRetry = ShouldRetry
	if r.settings.retry != nil && r.settings.retry.shouldRetry != nil {
		shouldRetry = r.settings.retry.shouldRetry
	}
	if shouldRetry(err) {
		// This will "close" the existing stream and immediately attempt to
		// reopen the stream, but will backoff if further attempts are necessary.
		// When Reopening the stream only failed readID will be added to stream.
		return r.reopenStream(getActiveRange(r))
	}
	return err
}

// reopenStream "closes" the existing stream and attempts to reopen a stream and
// sets the Reader's stream and cancelStream properties in the process.
func (r *gRPCBidiReader) reopenStream(failSpec []rangeSpec) error {
	// Close existing stream and initialize new stream with updated offset.
	if r.cancel != nil {
		r.cancel()
	}

	res, cancel, err := r.reopen()
	if err != nil {
		return err
	}
	r.stream = res.stream
	r.cancel = cancel
	r.readHandle = res.response.GetReadHandle().GetHandle()
	if failSpec != nil {
		r.data <- failSpec
	}
	return nil
}

// Add will add current range to stream.
func (mr *gRPCBidiReader) add(output io.Writer, offset, limit int64, callback func(int64, int64, error)) {
	mr.mu.Lock()
	objectSize := mr.objectSize
	mr.mu.Unlock()

	if offset > objectSize {
		callback(offset, limit, fmt.Errorf("offset larger than size of object: %v", objectSize))
		return
	}
	if limit < 0 {
		callback(offset, limit, fmt.Errorf("limit can't be negative"))
		return
	}
	mr.mu.Lock()
	curentID := (*mr).readID
	(*mr).readID++
	if !mr.done {
		spec := rangeSpec{readID: curentID, writer: output, offset: offset, limit: limit, bytesWritten: 0, callback: callback}
		mr.mp[curentID] = spec
		mr.activeTask++
		mr.data <- []rangeSpec{spec}
	} else {
		callback(offset, limit, fmt.Errorf("stream is closed, can't add range"))
	}
	mr.mu.Unlock()
}

func (mr *gRPCBidiReader) wait() {
	mr.mu.Lock()
	keepWaiting := len(mr.mp) != 0 && mr.activeTask != 0
	mr.mu.Unlock()

	for keepWaiting {
		mr.mu.Lock()
		keepWaiting = len(mr.mp) != 0 && mr.activeTask != 0
		mr.mu.Unlock()
	}
}

// Close will notify stream manager goroutine that the reader has been closed, if it's still running.
func (mr *gRPCBidiReader) close() error {
	if mr.cancel != nil {
		mr.cancel()
	}
	mr.mu.Lock()
	mr.done = true
	mr.activeTask = 0
	mr.mu.Unlock()
	mr.closeReceiver <- true
	mr.closeManager <- true
	return nil
}

func (mrr *gRPCBidiReader) getHandle() []byte {
	return mrr.readHandle
}

func (c *grpcStorageClient) NewRangeReader(ctx context.Context, params *newRangeReaderParams, opts ...storageOption) (r *Reader, err error) {
	// If bidi reads was not selected, use the legacy read object API.
	if !c.config.grpcBidiReads {
		return c.NewRangeReaderReadObject(ctx, params, opts...)
	}

	ctx = trace.StartSpan(ctx, "cloud.google.com/go/storage.grpcStorageClient.NewRangeReader")
	defer func() { trace.EndSpan(ctx, err) }()

	s := callSettings(c.settings, opts...)

	s.gax = append(s.gax, gax.WithGRPCOptions(
		grpc.ForceCodecV2(bytesCodecV2{}),
	))

	if s.userProject != "" {
		ctx = setUserProjectMetadata(ctx, s.userProject)
	}

	b := bucketResourceName(globalProjectAlias, params.bucket)

	// Create a BidiReadObjectRequest.
	spec := &storagepb.BidiReadObjectSpec{
		Bucket:                    b,
		Object:                    params.object,
		CommonObjectRequestParams: toProtoCommonObjectRequestParams(params.encryptionKey),
	}
	if err := applyCondsProto("gRPCReader.NewRangeReader", params.gen, params.conds, spec); err != nil {
		return nil, err
	}
	if params.handle != nil {
		spec.ReadHandle = &storagepb.BidiReadHandle{
			Handle: *params.handle,
		}
	}
	req := &storagepb.BidiReadObjectRequest{
		ReadObjectSpec: spec,
	}
	ctx = gax.InsertMetadataIntoOutgoingContext(ctx, contextMetadataFromBidiReadObject(req)...)

	// Define a function that initiates a Read with offset and length, assuming
	// we have already read seen bytes.
	reopen := func(seen int64) (*readStreamResponse, context.CancelFunc, error) {
		// If the context has already expired, return immediately without making
		// we call.
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		cc, cancel := context.WithCancel(ctx)

		// BidiReadObject can take multiple ranges, but we just request one in this case.
		readRange := &storagepb.ReadRange{
			ReadOffset: params.offset + seen,
			ReadId:     1,
		}

		// Only set a ReadLength if length is greater than zero, because <= 0 means
		// to read it all.
		if params.length > 0 {
			readRange.ReadLength = params.length - seen
		}

		req.ReadRanges = []*storagepb.ReadRange{readRange}

		var stream storagepb.Storage_BidiReadObjectClient
		var err error
		var decoder *readResponseDecoder

		err = run(cc, func(ctx context.Context) error {
			stream, err = c.raw.BidiReadObject(ctx, s.gax...)
			if err != nil {
				return err
			}
			if err := stream.Send(req); err != nil {
				return err
			}
			// Oneshot reads can close the client->server side immediately.
			if err := stream.CloseSend(); err != nil {
				return err
			}

			// Receive the message into databuf as a wire-encoded message so we can
			// use a custom decoder to avoid an extra copy at the protobuf layer.
			databufs := mem.BufferSlice{}
			err := stream.RecvMsg(&databufs)
			// These types of errors show up on the RecvMsg call, rather than the
			// initialization of the stream via BidiReadObject above.
			if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
				return ErrObjectNotExist
			}
			if err != nil {
				return err
			}
			// Use a custom decoder that uses protobuf unmarshalling for all
			// fields except the object data. Object data is handled separately
			// to avoid a copy.
			decoder = &readResponseDecoder{
				databufs: databufs,
			}
			err = decoder.readFullObjectResponse()
			return err
		}, s.retry, s.idempotent)
		if err != nil {
			// Close the stream context we just created to ensure we don't leak
			// resources.
			cancel()
			// Free any buffers.
			if decoder != nil && decoder.databufs != nil {
				decoder.databufs.Free()
			}
			return nil, nil, err
		}

		return &readStreamResponse{
			stream:  stream,
			decoder: decoder,
		}, cancel, nil
	}

	res, cancel, err := reopen(0)
	if err != nil {
		return nil, err
	}
	// The first message was Recv'd on stream open, use it to populate the
	// object metadata and read handle.
	msg := res.decoder.msg
	obj := msg.GetMetadata()
	handle := ReadHandle(msg.GetReadHandle().GetHandle())
	// This is the size of the entire object, even if only a range was requested.
	size := obj.GetSize()

	// Only support checksums when reading an entire object, not a range.
	var (
		wantCRC  uint32
		checkCRC bool
	)
	if checksums := obj.GetChecksums(); checksums != nil && checksums.Crc32C != nil {
		if params.offset == 0 && params.length < 0 {
			checkCRC = true
		}
		wantCRC = checksums.GetCrc32C()
	}

	startOffset := params.offset
	if params.offset < 0 {
		startOffset = size + params.offset
	}

	// The remaining bytes are the lesser of the requested range and all bytes
	// after params.offset.
	length := params.length
	if params.length > size || params.length < 0 {
		// if params.length < 0 (or larger than object size),
		// all remaining bytes were requested.
		length = size
	}
	remain := length - startOffset

	metadata := obj.GetMetadata()
	r = &Reader{
		Attrs: ReaderObjectAttrs{
			Size:            size,
			StartOffset:     startOffset,
			ContentType:     obj.GetContentType(),
			ContentEncoding: obj.GetContentEncoding(),
			CacheControl:    obj.GetCacheControl(),
			LastModified:    obj.GetUpdateTime().AsTime(),
			Metageneration:  obj.GetMetageneration(),
			Generation:      obj.GetGeneration(),
			CRC32C:          wantCRC,
		},
		objectMetadata: &metadata,
		reader: &gRPCReader{
			stream: res.stream,
			reopen: reopen,
			cancel: cancel,
			size:   size,
			// Preserve the decoder to read out object data when Read/WriteTo is called.
			currMsg:   res.decoder,
			settings:  s,
			zeroRange: params.length == 0,
			wantCRC:   wantCRC,
			checkCRC:  checkCRC,
		},
		checkCRC: checkCRC,
		handle:   &handle,
		remain:   remain,
	}

	// For a zero-length request, explicitly close the stream and set remaining
	// bytes to zero.
	if params.length == 0 {
		r.remain = 0
		r.reader.Close()
	}

	return r, nil
}

func (c *grpcStorageClient) OpenWriter(params *openWriterParams, opts ...storageOption) (*io.PipeWriter, error) {
	var offset int64
	errorf := params.setError
	setObj := params.setObj
	pr, pw := io.Pipe()

	s := callSettings(c.settings, opts...)

	// This function reads the data sent to the pipe and sends sets of messages
	// on the gRPC client-stream as the buffer is filled.
	go func() {
		err := func() error {
			// Unless the user told us the content type, we have to determine it from
			// the first read.
			var r io.Reader = pr
			if params.attrs.ContentType == "" && !params.forceEmptyContentType {
				r, params.attrs.ContentType = gax.DetermineContentType(r)
			}

			var gw *gRPCWriter
			gw, err := newGRPCWriter(c, s, params, r)
			if err != nil {
				return err
			}

			// Loop until there is an error or the Object has been finalized.
			for {
				// Note: This blocks until either the buffer is full or EOF is read.
				recvd, doneReading, err := gw.read()
				if err != nil {
					return err
				}

				var o *storagepb.Object
				uploadBuff := func(ctx context.Context) error {
					obj, err := gw.uploadBuffer(recvd, offset, doneReading)
					o = obj
					return err
				}

				err = run(gw.ctx, uploadBuff, gw.settings.retry, s.idempotent)
				if err != nil {
					return err
				}
				offset += int64(recvd)

				// When we are done reading data without errors, set the object and
				// finish.
				if doneReading {
					// Build Object from server's response.
					setObj(newObjectFromProto(o))
					return nil
				}
			}
		}()

		// These calls are still valid if err is nil
		err = checkCanceled(err)
		errorf(err)
		pr.CloseWithError(err)
		close(params.donec)
	}()

	return pw, nil
}

// IAM methods.

func (c *grpcStorageClient) GetIamPolicy(ctx context.Context, resource string, version int32, opts ...storageOption) (*iampb.Policy, error) {
	// TODO: Need a way to set UserProject, potentially in X-Goog-User-Project system parameter.
	s := callSettings(c.settings, opts...)
	req := &iampb.GetIamPolicyRequest{
		Resource: bucketResourceName(globalProjectAlias, resource),
		Options: &iampb.GetPolicyOptions{
			RequestedPolicyVersion: version,
		},
	}
	var rp *iampb.Policy
	err := run(ctx, func(ctx context.Context) error {
		var err error
		rp, err = c.raw.GetIamPolicy(ctx, req, s.gax...)
		return err
	}, s.retry, s.idempotent)

	return rp, err
}

func (c *grpcStorageClient) SetIamPolicy(ctx context.Context, resource string, policy *iampb.Policy, opts ...storageOption) error {
	// TODO: Need a way to set UserProject, potentially in X-Goog-User-Project system parameter.
	s := callSettings(c.settings, opts...)

	req := &iampb.SetIamPolicyRequest{
		Resource: bucketResourceName(globalProjectAlias, resource),
		Policy:   policy,
	}

	return run(ctx, func(ctx context.Context) error {
		_, err := c.raw.SetIamPolicy(ctx, req, s.gax...)
		return err
	}, s.retry, s.idempotent)
}

func (c *grpcStorageClient) TestIamPermissions(ctx context.Context, resource string, permissions []string, opts ...storageOption) ([]string, error) {
	// TODO: Need a way to set UserProject, potentially in X-Goog-User-Project system parameter.
	s := callSettings(c.settings, opts...)
	req := &iampb.TestIamPermissionsRequest{
		Resource:    bucketResourceName(globalProjectAlias, resource),
		Permissions: permissions,
	}
	var res *iampb.TestIamPermissionsResponse
	err := run(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.raw.TestIamPermissions(ctx, req, s.gax...)
		return err
	}, s.retry, s.idempotent)
	if err != nil {
		return nil, err
	}
	return res.Permissions, nil
}

// HMAC Key methods are not implemented in gRPC client.

func (c *grpcStorageClient) GetHMACKey(ctx context.Context, project, accessID string, opts ...storageOption) (*HMACKey, error) {
	return nil, errMethodNotSupported
}

func (c *grpcStorageClient) ListHMACKeys(ctx context.Context, project, serviceAccountEmail string, showDeletedKeys bool, opts ...storageOption) *HMACKeysIterator {
	it := &HMACKeysIterator{
		ctx:       ctx,
		projectID: "",
		retry:     nil,
	}
	fetch := func(_ int, _ string) (token string, err error) {
		return "", errMethodNotSupported
	}
	it.pageInfo, it.nextFunc = iterator.NewPageInfo(
		fetch,
		func() int { return 0 },
		func() interface{} { return nil },
	)
	return it
}

func (c *grpcStorageClient) UpdateHMACKey(ctx context.Context, project, serviceAccountEmail, accessID string, attrs *HMACKeyAttrsToUpdate, opts ...storageOption) (*HMACKey, error) {
	return nil, errMethodNotSupported
}

func (c *grpcStorageClient) CreateHMACKey(ctx context.Context, project, serviceAccountEmail string, opts ...storageOption) (*HMACKey, error) {
	return nil, errMethodNotSupported
}

func (c *grpcStorageClient) DeleteHMACKey(ctx context.Context, project string, accessID string, opts ...storageOption) error {
	return errMethodNotSupported
}

// Notification methods are not implemented in gRPC client.

func (c *grpcStorageClient) ListNotifications(ctx context.Context, bucket string, opts ...storageOption) (n map[string]*Notification, err error) {
	return nil, errMethodNotSupported
}

func (c *grpcStorageClient) CreateNotification(ctx context.Context, bucket string, n *Notification, opts ...storageOption) (ret *Notification, err error) {
	return nil, errMethodNotSupported
}

func (c *grpcStorageClient) DeleteNotification(ctx context.Context, bucket string, id string, opts ...storageOption) (err error) {
	return errMethodNotSupported
}

// setUserProjectMetadata appends a project ID to the outgoing Context metadata
// via the x-goog-user-project system parameter defined at
// https://cloud.google.com/apis/docs/system-parameters. This is only for
// billing purposes, and is generally optional, except for requester-pays
// buckets.
func setUserProjectMetadata(ctx context.Context, project string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "x-goog-user-project", project)
}

type readStreamResponse struct {
	stream  storagepb.Storage_BidiReadObjectClient
	decoder *readResponseDecoder
}

type bidiReadStreamResponse struct {
	stream   storagepb.Storage_BidiReadObjectClient
	response *storagepb.BidiReadObjectResponse
}

type gRPCBidiReader struct {
	stream           storagepb.Storage_BidiReadObjectClient
	cancel           context.CancelFunc
	settings         *settings
	readHandle       ReadHandle
	readID           int64
	reopen           func() (*bidiReadStreamResponse, context.CancelFunc, error)
	readSpec         *storagepb.BidiReadObjectSpec
	data             chan []rangeSpec
	ctx              context.Context
	closeReceiver    chan bool
	closeManager     chan bool
	managerRetry     chan bool
	receiverRetry    chan bool
	mu               sync.Mutex          // protects all vars in gRPCBidiReader from concurrent access
	mp               map[int64]rangeSpec // always use the mutex when accessing the map
	done             bool                // always use the mutex when accessing this variable
	activeTask       int64               // always use the mutex when accessing this variable
	objectSize       int64               // always use the mutex when accessing this variable
	retrier          func(error, string)
	streamRecreation bool // This helps us identify if stream recreation is in progress or not. If stream recreation gets called from two goroutine then this will stop second one.
}

// gRPCReader is used by storage.Reader if the experimental option WithGRPCBidiReads is passed.
type gRPCReader struct {
	seen, size int64
	zeroRange  bool
	stream     storagepb.Storage_BidiReadObjectClient
	reopen     func(seen int64) (*readStreamResponse, context.CancelFunc, error)
	leftovers  []byte
	currMsg    *readResponseDecoder // decoder for the current message
	cancel     context.CancelFunc
	settings   *settings
	checkCRC   bool   // should we check the CRC?
	wantCRC    uint32 // the CRC32c value the server sent in the header
	gotCRC     uint32 // running crc
}

// Update the running CRC with the data in the slice, if CRC checking was enabled.
func (r *gRPCReader) updateCRC(b []byte) {
	if r.checkCRC {
		r.gotCRC = crc32.Update(r.gotCRC, crc32cTable, b)
	}
}

// Checks whether the CRC matches at the conclusion of a read, if CRC checking was enabled.
func (r *gRPCReader) runCRCCheck() error {
	if r.checkCRC && r.gotCRC != r.wantCRC {
		return fmt.Errorf("storage: bad CRC on read: got %d, want %d", r.gotCRC, r.wantCRC)
	}
	return nil
}

// Read reads bytes into the user's buffer from an open gRPC stream.
func (r *gRPCReader) Read(p []byte) (int, error) {
	// The entire object has been read by this reader, check the checksum if
	// necessary and return EOF.
	if r.size == r.seen || r.zeroRange {
		if err := r.runCRCCheck(); err != nil {
			return 0, err
		}
		return 0, io.EOF
	}

	// No stream to read from, either never initialized or Close was called.
	// Note: There is a potential concurrency issue if multiple routines are
	// using the same reader. One encounters an error and the stream is closed
	// and then reopened while the other routine attempts to read from it.
	if r.stream == nil {
		return 0, fmt.Errorf("storage: reader has been closed")
	}

	var n int

	// If there is data remaining in the current message, return what was
	// available to conform to the Reader
	// interface: https://pkg.go.dev/io#Reader.
	if !r.currMsg.done {
		n = r.currMsg.readAndUpdateCRC(p, func(b []byte) {
			r.updateCRC(b)
		})
		r.seen += int64(n)
		return n, nil
	}

	// Attempt to Recv the next message on the stream.
	// This will update r.currMsg with the decoder for the new message.
	err := r.recv()
	if err != nil {
		return 0, err
	}

	// TODO: Determine if we need to capture incremental CRC32C for this
	// chunk. The Object CRC32C checksum is captured when directed to read
	// the entire Object. If directed to read a range, we may need to
	// calculate the range's checksum for verification if the checksum is
	// present in the response here.
	// TODO: Figure out if we need to support decompressive transcoding
	// https://cloud.google.com/storage/docs/transcoding.

	n = r.currMsg.readAndUpdateCRC(p, func(b []byte) {
		r.updateCRC(b)
	})
	r.seen += int64(n)
	return n, nil
}

// WriteTo writes all the data requested by the Reader into w, implementing
// io.WriterTo.
func (r *gRPCReader) WriteTo(w io.Writer) (int64, error) {
	// The entire object has been read by this reader, check the checksum if
	// necessary and return nil.
	if r.size == r.seen || r.zeroRange {
		if err := r.runCRCCheck(); err != nil {
			return 0, err
		}
		return 0, nil
	}

	// No stream to read from, either never initialized or Close was called.
	// Note: There is a potential concurrency issue if multiple routines are
	// using the same reader. One encounters an error and the stream is closed
	// and then reopened while the other routine attempts to read from it.
	if r.stream == nil {
		return 0, fmt.Errorf("storage: reader has been closed")
	}

	// Track bytes written during before call.
	var alreadySeen = r.seen

	// Write any already received message to the stream. There will be some leftovers from the
	// original NewRangeReader call.
	if r.currMsg != nil && !r.currMsg.done {
		written, err := r.currMsg.writeToAndUpdateCRC(w, func(b []byte) {
			r.updateCRC(b)
		})
		r.seen += int64(written)
		r.currMsg = nil
		if err != nil {
			return r.seen - alreadySeen, err
		}
	}

	// Loop and receive additional messages until the entire data is written.
	for {
		// Attempt to receive the next message on the stream.
		// Will terminate with io.EOF once data has all come through.
		// recv() handles stream reopening and retry logic so no need for retries here.
		err := r.recv()
		if err != nil {
			if err == io.EOF {
				// We are done; check the checksum if necessary and return.
				err = r.runCRCCheck()
			}
			return r.seen - alreadySeen, err
		}

		// TODO: Determine if we need to capture incremental CRC32C for this
		// chunk. The Object CRC32C checksum is captured when directed to read
		// the entire Object. If directed to read a range, we may need to
		// calculate the range's checksum for verification if the checksum is
		// present in the response here.
		// TODO: Figure out if we need to support decompressive transcoding
		// https://cloud.google.com/storage/docs/transcoding.
		written, err := r.currMsg.writeToAndUpdateCRC(w, func(b []byte) {
			r.updateCRC(b)
		})
		r.seen += int64(written)
		if err != nil {
			return r.seen - alreadySeen, err
		}
	}

}

// Close cancels the read stream's context in order for it to be closed and
// collected, and frees any currently in use buffers.
func (r *gRPCReader) Close() error {
	if r.cancel != nil {
		r.cancel()
	}
	r.currMsg = nil
	return nil
}

// recv attempts to Recv the next message on the stream and extract the object
// data that it contains. In the event that a retryable error is encountered,
// the stream will be closed, reopened, and RecvMsg again.
// This will attempt to Recv until one of the following is true:
//
// * Recv is successful
// * A non-retryable error is encountered
// * The Reader's context is canceled
//
// The last error received is the one that is returned, which could be from
// an attempt to reopen the stream.

func (r *gRPCReader) recv() error {
	databufs := mem.BufferSlice{}
	err := r.stream.RecvMsg(&databufs)
	var shouldRetry = ShouldRetry
	if r.settings.retry != nil && r.settings.retry.shouldRetry != nil {
		shouldRetry = r.settings.retry.shouldRetry
	}
	if err != nil && shouldRetry(err) {
		// This will "close" the existing stream and immediately attempt to
		// reopen the stream, but will backoff if further attempts are necessary.
		// Reopening the stream Recvs the first message, so if retrying is
		// successful, r.currMsg will be updated to include the new data.
		return r.reopenStream()
	}

	if err != nil {
		return err
	}

	r.currMsg = &readResponseDecoder{databufs: databufs}
	return r.currMsg.readFullObjectResponse()
}

// ReadObjectResponse field and subfield numbers.
const (
	// Top level fields.
	metadataField        = protowire.Number(4)
	objectRangeDataField = protowire.Number(6)
	readHandleField      = protowire.Number(7)
	// Nested in ObjectRangeData
	checksummedDataField = protowire.Number(1)
	readRangeField       = protowire.Number(2)
	rangeEndField        = protowire.Number(3)
	// Nested in ObjectRangeData.ChecksummedData
	checksummedDataContentField = protowire.Number(1)
	checksummedDataCRC32CField  = protowire.Number(2)
)

// readResponseDecoder is a wrapper on the raw message, used to decode one message
// without copying object data. It also has methods to write out the resulting object
// data to the user application.
type readResponseDecoder struct {
	databufs mem.BufferSlice // raw bytes of the message being processed
	// Decoding offsets
	off     uint64 // offset in the messsage relative to the data as a whole
	currBuf int    // index of the current buffer being processed
	currOff uint64 // offset in the current buffer
	// Processed data
	msg         *storagepb.BidiReadObjectResponse // processed response message with all fields other than object data populated
	dataOffsets bufferSliceOffsets                // offsets of the object data in the message.
	done        bool                              // true if the data has been completely read.
}

type bufferSliceOffsets struct {
	startBuf, endBuf int    // indices of start and end buffers of object data in the msg
	startOff, endOff uint64 // offsets within these buffers where the data starts and ends.
	currBuf          int    // index of current buffer being read out to the user application.
	currOff          uint64 // offset of read in current buffer.
}

// peek ahead 10 bytes from the current offset in the databufs. This will return a
// slice of the current buffer if the bytes are all in one buffer, but will copy
// the bytes into a new buffer if the distance is split across buffers. Use this
// to allow protowire methods to be used to parse tags & fixed values.
// The max length of a varint tag is 10 bytes, see
// https://protobuf.dev/programming-guides/encoding/#varints . Other int types
// are shorter.
func (d *readResponseDecoder) peek() []byte {
	b := d.databufs[d.currBuf].ReadOnlyData()
	// Check if the tag will fit in the current buffer. If not, copy the next 10
	// bytes into a new buffer to ensure that we can read the tag correctly
	// without it being divided between buffers.
	tagBuf := b[d.currOff:]
	remainingInBuf := len(tagBuf)
	// If we have less than 10 bytes remaining and are not in the final buffer,
	// copy up to 10 bytes ahead from the next buffer.
	if remainingInBuf < binary.MaxVarintLen64 && d.currBuf != len(d.databufs)-1 {
		tagBuf = d.copyNextBytes(10)
	}
	return tagBuf
}

// Copies up to next n bytes into a new buffer, or fewer if fewer bytes remain in the
// buffers overall. Does not advance offsets.
func (d *readResponseDecoder) copyNextBytes(n int) []byte {
	remaining := n
	if r := d.databufs.Len() - int(d.off); r < remaining {
		remaining = r
	}
	currBuf := d.currBuf
	currOff := d.currOff
	var buf []byte
	for remaining > 0 {
		b := d.databufs[currBuf].ReadOnlyData()
		remainingInCurr := len(b[currOff:])
		if remainingInCurr < remaining {
			buf = append(buf, b[currOff:]...)
			remaining -= remainingInCurr
			currBuf++
			currOff = 0
		} else {
			buf = append(buf, b[currOff:currOff+uint64(remaining)]...)
			remaining = 0
		}
	}
	return buf
}

// Advance current buffer & byte offset in the decoding by n bytes. Returns an error if we
// go past the end of the data.
func (d *readResponseDecoder) advanceOffset(n uint64) error {
	remaining := n
	for remaining > 0 {
		remainingInCurr := uint64(d.databufs[d.currBuf].Len()) - d.currOff
		if remainingInCurr <= remaining {
			remaining -= remainingInCurr
			d.currBuf++
			d.currOff = 0
		} else {
			d.currOff += remaining
			remaining = 0
		}
	}
	// If we have advanced past the end of the buffers, something went wrong.
	if (d.currBuf == len(d.databufs) && d.currOff > 0) || d.currBuf > len(d.databufs) {
		return errors.New("decoding: truncated message, cannot advance offset")
	}
	d.off += n
	return nil

}

// This copies object data from the message into the buffer and returns the number of
// bytes copied. The data offsets are incremented in the message. The updateCRC
// function is called on the copied bytes.
func (d *readResponseDecoder) readAndUpdateCRC(p []byte, updateCRC func([]byte)) int {
	// For a completely empty message, just return 0
	if len(d.databufs) == 0 {
		return 0
	}
	databuf := d.databufs[d.dataOffsets.currBuf]
	startOff := d.dataOffsets.currOff
	var b []byte
	if d.dataOffsets.currBuf == d.dataOffsets.endBuf {
		b = databuf.ReadOnlyData()[startOff:d.dataOffsets.endOff]
	} else {
		b = databuf.ReadOnlyData()[startOff:]
	}
	n := copy(p, b)
	updateCRC(b[:n])
	d.dataOffsets.currOff += uint64(n)

	// We've read all the data from this message. Free the underlying buffers.
	if d.dataOffsets.currBuf == d.dataOffsets.endBuf && d.dataOffsets.currOff == d.dataOffsets.endOff {
		d.done = true
		d.databufs.Free()
	}
	// We are at the end of the current buffer
	if d.dataOffsets.currBuf != d.dataOffsets.endBuf && d.dataOffsets.currOff == uint64(databuf.Len()) {
		d.dataOffsets.currOff = 0
		d.dataOffsets.currBuf++
	}
	return n
}

func (d *readResponseDecoder) writeToAndUpdateCRC(w io.Writer, updateCRC func([]byte)) (int64, error) {
	// For a completely empty message, just return 0
	if len(d.databufs) == 0 {
		return 0, nil
	}
	var written int64
	for !d.done {
		databuf := d.databufs[d.dataOffsets.currBuf]
		startOff := d.dataOffsets.currOff
		var b []byte
		if d.dataOffsets.currBuf == d.dataOffsets.endBuf {
			b = databuf.ReadOnlyData()[startOff:d.dataOffsets.endOff]
		} else {
			b = databuf.ReadOnlyData()[startOff:]
		}
		var n int
		// Write all remaining data from the current buffer
		n, err := w.Write(b)
		written += int64(n)
		updateCRC(b)
		if err != nil {
			return written, err
		}
		d.dataOffsets.currOff = 0
		// We've read all the data from this message.
		if d.dataOffsets.currBuf == d.dataOffsets.endBuf {
			d.done = true
			d.databufs.Free()
		} else {
			d.dataOffsets.currBuf++
		}
	}
	return written, nil
}

// Consume the next available tag in the input data and return the field number and type.
// Advances the relevant offsets in the data.
func (d *readResponseDecoder) consumeTag() (protowire.Number, protowire.Type, error) {
	tagBuf := d.peek()

	// Consume the next tag. This will tell us which field is next in the
	// buffer, its type, and how much space it takes up.
	fieldNum, fieldType, tagLength := protowire.ConsumeTag(tagBuf)
	if tagLength < 0 {
		return 0, 0, protowire.ParseError(tagLength)
	}
	// Update the offsets and current buffer depending on the tag length.
	if err := d.advanceOffset(uint64(tagLength)); err != nil {
		return 0, 0, fmt.Errorf("consuming tag: %w", err)
	}
	return fieldNum, fieldType, nil
}

// Consume a varint that represents the length of a bytes field. Return the length of
// the data, and advance the offsets by the length of the varint.
func (d *readResponseDecoder) consumeVarint() (uint64, error) {
	tagBuf := d.peek()

	// Consume the next tag. This will tell us which field is next in the
	// buffer, its type, and how much space it takes up.
	dataLength, tagLength := protowire.ConsumeVarint(tagBuf)
	if tagLength < 0 {
		return 0, protowire.ParseError(tagLength)
	}

	// Update the offsets and current buffer depending on the tag length.
	d.advanceOffset(uint64(tagLength))
	return dataLength, nil
}

func (d *readResponseDecoder) consumeFixed32() (uint32, error) {
	valueBuf := d.peek()

	// Consume the next tag. This will tell us which field is next in the
	// buffer, its type, and how much space it takes up.
	value, tagLength := protowire.ConsumeFixed32(valueBuf)
	if tagLength < 0 {
		return 0, protowire.ParseError(tagLength)
	}

	// Update the offsets and current buffer depending on the tag length.
	d.advanceOffset(uint64(tagLength))
	return value, nil
}

func (d *readResponseDecoder) consumeFixed64() (uint64, error) {
	valueBuf := d.peek()

	// Consume the next tag. This will tell us which field is next in the
	// buffer, its type, and how much space it takes up.
	value, tagLength := protowire.ConsumeFixed64(valueBuf)
	if tagLength < 0 {
		return 0, protowire.ParseError(tagLength)
	}

	// Update the offsets and current buffer depending on the tag length.
	d.advanceOffset(uint64(tagLength))
	return value, nil
}

// Consume any field values up to the end offset provided and don't return anything.
// This is used to skip any values which are not going to be used.
// msgEndOff is indexed in terms of the overall data across all buffers.
func (d *readResponseDecoder) consumeFieldValue(fieldNum protowire.Number, fieldType protowire.Type) error {
	// reimplement protowire.ConsumeFieldValue without the extra case for groups (which
	// are are complicted and not a thing in proto3).
	var err error
	switch fieldType {
	case protowire.VarintType:
		_, err = d.consumeVarint()
	case protowire.Fixed32Type:
		_, err = d.consumeFixed32()
	case protowire.Fixed64Type:
		_, err = d.consumeFixed64()
	case protowire.BytesType:
		_, err = d.consumeBytes()
	default:
		return fmt.Errorf("unknown field type %v in field %v", fieldType, fieldNum)
	}
	if err != nil {
		return fmt.Errorf("consuming field %v of type %v: %w", fieldNum, fieldType, err)
	}

	return nil
}

// Consume a bytes field from the input. Returns offsets for the data in the buffer slices
// and an error.
func (d *readResponseDecoder) consumeBytes() (bufferSliceOffsets, error) {
	// m is the length of the data past the tag.
	m, err := d.consumeVarint()
	if err != nil {
		return bufferSliceOffsets{}, fmt.Errorf("consuming bytes field: %w", err)
	}
	offsets := bufferSliceOffsets{
		startBuf: d.currBuf,
		startOff: d.currOff,
		currBuf:  d.currBuf,
		currOff:  d.currOff,
	}

	// Advance offsets to lengths of bytes field and capture where we end.
	d.advanceOffset(m)
	offsets.endBuf = d.currBuf
	offsets.endOff = d.currOff
	return offsets, nil
}

// Consume a bytes field from the input and copy into a new buffer if
// necessary (if the data is split across buffers in databuf).  This can be
// used to leverage proto.Unmarshal for small bytes fields (i.e. anything
// except object data).
func (d *readResponseDecoder) consumeBytesCopy() ([]byte, error) {
	// m is the length of the bytes data.
	m, err := d.consumeVarint()
	if err != nil {
		return nil, fmt.Errorf("consuming varint: %w", err)
	}
	// Copy the data into a buffer and advance the offset
	b := d.copyNextBytes(int(m))
	if err := d.advanceOffset(m); err != nil {
		return nil, fmt.Errorf("advancing offset: %w", err)
	}
	return b, nil
}

// readFullObjectResponse returns the BidiReadObjectResponse that is encoded in the
// wire-encoded message buffer b, or an error if the message is invalid.
// This must be used on the first recv of an object as it may contain all fields
// of BidiReadObjectResponse, and we use or pass on those fields to the user.
// This function is essentially identical to proto.Unmarshal, except it aliases
// the data in the input []byte. If the proto library adds a feature to
// Unmarshal that does that, this function can be dropped.
func (d *readResponseDecoder) readFullObjectResponse() error {
	msg := &storagepb.BidiReadObjectResponse{}

	// Loop over the entire message, extracting fields as we go. This does not
	// handle field concatenation, in which the contents of a single field
	// are split across multiple protobuf tags.
	for d.off < uint64(d.databufs.Len()) {
		fieldNum, fieldType, err := d.consumeTag()
		if err != nil {
			return fmt.Errorf("consuming next tag: %w", err)
		}

		// Unmarshal the field according to its type. Only fields that are not
		// nil will be present.
		switch {
		// This is a repeated field, so it can occur more than once. But, for now
		// we can just take the first range per message since Reader only requests
		// a single range.
		// See https://protobuf.dev/programming-guides/encoding/#optional
		// TODO: support multiple ranges once integrated with MultiRangeDownloader.
		case fieldNum == objectRangeDataField && fieldType == protowire.BytesType:
			// The object data field was found. Initialize the data ranges assuming
			// exactly one range in the message.
			msg.ObjectDataRanges = []*storagepb.ObjectRangeData{{ChecksummedData: &storagepb.ChecksummedData{}, ReadRange: &storagepb.ReadRange{}}}
			bytesFieldLen, err := d.consumeVarint()
			if err != nil {
				return fmt.Errorf("consuming bytes: %v", err)
			}
			var contentEndOff = d.off + bytesFieldLen
			for d.off < contentEndOff {
				gotNum, gotTyp, err := d.consumeTag()
				if err != nil {
					return fmt.Errorf("consuming objectRangeData tag: %w", err)
				}

				switch {
				case gotNum == checksummedDataField && gotTyp == protowire.BytesType:
					checksummedDataFieldLen, err := d.consumeVarint()
					if err != nil {
						return fmt.Errorf("consuming bytes: %v", err)
					}
					var checksummedDataEndOff = d.off + checksummedDataFieldLen
					for d.off < checksummedDataEndOff {
						gotNum, gotTyp, err := d.consumeTag()
						if err != nil {
							return fmt.Errorf("consuming checksummedData tag: %w", err)
						}
						switch {
						case gotNum == checksummedDataContentField && gotTyp == protowire.BytesType:
							// Get the offsets of the content bytes.
							d.dataOffsets, err = d.consumeBytes()
							if err != nil {
								return fmt.Errorf("invalid BidiReadObjectResponse.ChecksummedData.Content: %w", err)
							}
						case gotNum == checksummedDataCRC32CField && gotTyp == protowire.Fixed32Type:
							v, err := d.consumeFixed32()
							if err != nil {
								return fmt.Errorf("invalid BidiReadObjectResponse.ChecksummedData.Crc32C: %w", err)
							}
							msg.ObjectDataRanges[0].ChecksummedData.Crc32C = &v
						default:
							err := d.consumeFieldValue(gotNum, gotTyp)
							if err != nil {
								return fmt.Errorf("invalid field in BidiReadObjectResponse.ChecksummedData: %w", err)
							}
						}
					}
				case gotNum == readRangeField && gotTyp == protowire.BytesType:
					buf, err := d.consumeBytesCopy()
					if err != nil {
						return fmt.Errorf("invalid ObjectDataRange.ReadRange: %v", err)
					}

					if err := proto.Unmarshal(buf, msg.ObjectDataRanges[0].ReadRange); err != nil {
						return err
					}
				case gotNum == rangeEndField && gotTyp == protowire.VarintType: // proto encodes bool as int32
					b, err := d.consumeVarint()
					if err != nil {
						return fmt.Errorf("invalid ObjectDataRange.RangeEnd: %w", err)
					}
					msg.ObjectDataRanges[0].RangeEnd = protowire.DecodeBool(b)
				}

			}
		case fieldNum == metadataField && fieldType == protowire.BytesType:
			msg.Metadata = &storagepb.Object{}
			buf, err := d.consumeBytesCopy()
			if err != nil {
				return fmt.Errorf("invalid BidiReadObjectResponse.Metadata: %v", err)
			}

			if err := proto.Unmarshal(buf, msg.Metadata); err != nil {
				return err
			}
		case fieldNum == readHandleField && fieldType == protowire.BytesType:
			msg.ReadHandle = &storagepb.BidiReadHandle{}
			buf, err := d.consumeBytesCopy()
			if err != nil {
				return fmt.Errorf("invalid BidiReadObjectResponse.ReadHandle: %v", err)
			}

			if err := proto.Unmarshal(buf, msg.ReadHandle); err != nil {
				return err
			}
		default:
			err := d.consumeFieldValue(fieldNum, fieldType)
			if err != nil {
				return fmt.Errorf("invalid field in BidiReadObjectResponse: %w", err)
			}
		}
	}
	d.msg = msg

	return nil
}

// reopenStream "closes" the existing stream and attempts to reopen a stream and
// sets the Reader's stream and cancelStream properties in the process.
func (r *gRPCReader) reopenStream() error {
	// Close existing stream and initialize new stream with updated offset.
	r.Close()

	res, cancel, err := r.reopen(r.seen)
	if err != nil {
		return err
	}
	r.stream = res.stream
	r.currMsg = res.decoder
	r.cancel = cancel
	return nil
}

func newGRPCWriter(c *grpcStorageClient, s *settings, params *openWriterParams, r io.Reader) (*gRPCWriter, error) {
	if params.attrs.Retention != nil {
		// TO-DO: remove once ObjectRetention is available - see b/308194853
		return nil, status.Errorf(codes.Unimplemented, "storage: object retention is not supported in gRPC")
	}

	size := googleapi.MinUploadChunkSize
	// A completely bufferless upload (params.chunkSize <= 0) is not possible in
	// gRPC because the buffer must be provided to the message. Use the minimum
	// size possible.
	if params.chunkSize > 0 {
		size = params.chunkSize
	}

	// Round up chunksize to nearest 256KiB
	if size%googleapi.MinUploadChunkSize != 0 {
		size += googleapi.MinUploadChunkSize - (size % googleapi.MinUploadChunkSize)
	}

	if s.userProject != "" {
		params.ctx = setUserProjectMetadata(params.ctx, s.userProject)
	}

	spec := &storagepb.WriteObjectSpec{
		Resource:   params.attrs.toProtoObject(params.bucket),
		Appendable: proto.Bool(params.append),
	}
	// WriteObject doesn't support the generation condition, so use default.
	if err := applyCondsProto("WriteObject", defaultGen, params.conds, spec); err != nil {
		return nil, err
	}

	return &gRPCWriter{
		buf:                   make([]byte, size),
		c:                     c,
		ctx:                   params.ctx,
		reader:                r,
		bucket:                params.bucket,
		attrs:                 params.attrs,
		conds:                 params.conds,
		spec:                  spec,
		encryptionKey:         params.encryptionKey,
		settings:              s,
		progress:              params.progress,
		sendCRC32C:            params.sendCRC32C,
		forceOneShot:          params.chunkSize <= 0,
		forceEmptyContentType: params.forceEmptyContentType,
		append:                params.append,
	}, nil
}

// gRPCWriter is a wrapper around the the gRPC client-stream API that manages
// sending chunks of data provided by the user over the stream.
type gRPCWriter struct {
	c      *grpcStorageClient
	buf    []byte
	reader io.Reader

	ctx context.Context

	bucket        string
	attrs         *ObjectAttrs
	conds         *Conditions
	spec          *storagepb.WriteObjectSpec
	encryptionKey []byte
	settings      *settings
	progress      func(int64)

	sendCRC32C            bool
	forceOneShot          bool
	forceEmptyContentType bool
	append                bool

	streamSender gRPCBidiWriteBufferSender
}

func bucketContext(ctx context.Context, bucket string) context.Context {
	hds := []string{"x-goog-request-params", fmt.Sprintf("bucket=projects/_/buckets/%s", url.QueryEscape(bucket))}
	return gax.InsertMetadataIntoOutgoingContext(ctx, hds...)
}

// drainInboundStream calls stream.Recv() repeatedly until an error is returned.
// It returns the last Resource received on the stream, or nil if no Resource
// was returned. drainInboundStream always returns a non-nil error. io.EOF
// indicates all messages were successfully read.
func drainInboundStream(stream storagepb.Storage_BidiWriteObjectClient) (object *storagepb.Object, err error) {
	for err == nil {
		var resp *storagepb.BidiWriteObjectResponse
		resp, err = stream.Recv()
		// GetResource() returns nil on a nil response
		if resp.GetResource() != nil {
			object = resp.GetResource()
		}
	}
	return object, err
}

func bidiWriteObjectRequest(buf []byte, offset int64, flush, finishWrite bool) *storagepb.BidiWriteObjectRequest {
	return &storagepb.BidiWriteObjectRequest{
		Data: &storagepb.BidiWriteObjectRequest_ChecksummedData{
			ChecksummedData: &storagepb.ChecksummedData{
				Content: buf,
			},
		},
		WriteOffset: offset,
		FinishWrite: finishWrite,
		Flush:       flush,
		StateLookup: flush,
	}
}

type gRPCBidiWriteBufferSender interface {
	// sendBuffer implementations should upload buf, respecting flush and
	// finishWrite. Callers must guarantee that buf is not too long to fit in a
	// gRPC message.
	//
	// If flush is true, implementations must not return until the data in buf is
	// stable. If finishWrite is true, implementations must return the object on
	// success.
	sendBuffer(buf []byte, offset int64, flush, finishWrite bool) (*storagepb.Object, error)
}

type gRPCOneshotBidiWriteBufferSender struct {
	ctx          context.Context
	firstMessage *storagepb.BidiWriteObjectRequest
	raw          *gapic.Client
	stream       storagepb.Storage_BidiWriteObjectClient
	settings     *settings
}

func (w *gRPCWriter) newGRPCOneshotBidiWriteBufferSender() (*gRPCOneshotBidiWriteBufferSender, error) {
	firstMessage := &storagepb.BidiWriteObjectRequest{
		FirstMessage: &storagepb.BidiWriteObjectRequest_WriteObjectSpec{
			WriteObjectSpec: w.spec,
		},
		CommonObjectRequestParams: toProtoCommonObjectRequestParams(w.encryptionKey),
		// For a non-resumable upload, checksums must be sent in this message.
		// TODO: Currently the checksums are only sent on the first message
		// of the stream, but in the future, we must also support sending it
		// on the *last* message of the stream (instead of the first).
		ObjectChecksums: toProtoChecksums(w.sendCRC32C, w.attrs),
	}

	return &gRPCOneshotBidiWriteBufferSender{
		ctx:          bucketContext(w.ctx, w.bucket),
		firstMessage: firstMessage,
		raw:          w.c.raw,
		settings:     w.settings,
	}, nil
}

func (s *gRPCOneshotBidiWriteBufferSender) sendBuffer(buf []byte, offset int64, flush, finishWrite bool) (obj *storagepb.Object, err error) {
	var firstMessage *storagepb.BidiWriteObjectRequest
	if s.stream == nil {
		s.stream, err = s.raw.BidiWriteObject(s.ctx, s.settings.gax...)
		if err != nil {
			return
		}
		firstMessage = s.firstMessage
	}
	req := bidiWriteObjectRequest(buf, offset, flush, finishWrite)
	if firstMessage != nil {
		proto.Merge(req, firstMessage)
	}

	sendErr := s.stream.Send(req)
	if sendErr != nil {
		obj, err = drainInboundStream(s.stream)
		s.stream = nil
		if sendErr != io.EOF {
			err = sendErr
		}
		return
	}
	// Oneshot uploads assume all flushes succeed

	if finishWrite {
		s.stream.CloseSend()
		// Oneshot uploads only read from the response stream on completion or
		// failure
		obj, err = drainInboundStream(s.stream)
		s.stream = nil
		if err == io.EOF {
			err = nil
		}
	}
	return
}

type gRPCResumableBidiWriteBufferSender struct {
	ctx               context.Context
	queryRetry        *retryConfig
	upid              string
	progress          func(int64)
	raw               *gapic.Client
	forceFirstMessage bool
	stream            storagepb.Storage_BidiWriteObjectClient
	flushOffset       int64
	settings          *settings
}

func (w *gRPCWriter) newGRPCResumableBidiWriteBufferSender() (*gRPCResumableBidiWriteBufferSender, error) {
	req := &storagepb.StartResumableWriteRequest{
		WriteObjectSpec:           w.spec,
		CommonObjectRequestParams: toProtoCommonObjectRequestParams(w.encryptionKey),
		// TODO: Currently the checksums are only sent on the request to initialize
		// the upload, but in the future, we must also support sending it
		// on the *last* message of the stream.
		ObjectChecksums: toProtoChecksums(w.sendCRC32C, w.attrs),
	}

	ctx := bucketContext(w.ctx, w.bucket)
	var upid string
	err := run(ctx, func(ctx context.Context) error {
		upres, err := w.c.raw.StartResumableWrite(ctx, req, w.settings.gax...)
		upid = upres.GetUploadId()
		return err
	}, w.settings.retry, w.settings.idempotent)
	if err != nil {
		return nil, err
	}

	// Set up an initial connection for the 0 offset, so we don't query state
	// unnecessarily for the first buffer. If we fail, we'll just retry in the
	// normal connect path.
	stream, err := w.c.raw.BidiWriteObject(ctx, w.settings.gax...)
	if err != nil {
		stream = nil
	}

	return &gRPCResumableBidiWriteBufferSender{
		ctx:               ctx,
		queryRetry:        w.settings.retry,
		upid:              upid,
		progress:          w.progress,
		raw:               w.c.raw,
		forceFirstMessage: true,
		stream:            stream,
		settings:          w.settings,
	}, nil
}

// queryProgress is a helper that queries the status of the resumable upload
// associated with the given upload ID.
func (s *gRPCResumableBidiWriteBufferSender) queryProgress() (int64, error) {
	var persistedSize int64
	err := run(s.ctx, func(ctx context.Context) error {
		q, err := s.raw.QueryWriteStatus(ctx, &storagepb.QueryWriteStatusRequest{
			UploadId: s.upid,
		}, s.settings.gax...)
		// q.GetPersistedSize() will return 0 if q is nil.
		persistedSize = q.GetPersistedSize()
		return err
	}, s.queryRetry, true)

	return persistedSize, err
}

func (s *gRPCResumableBidiWriteBufferSender) sendBuffer(buf []byte, offset int64, flush, finishWrite bool) (obj *storagepb.Object, err error) {
	reconnected := false
	if s.stream == nil {
		// Determine offset and reconnect
		s.flushOffset, err = s.queryProgress()
		if err != nil {
			return
		}
		s.stream, err = s.raw.BidiWriteObject(s.ctx, s.settings.gax...)
		if err != nil {
			return
		}
		reconnected = true
	}

	// clean up buf. We'll still write the message if a flush/finishWrite was
	// requested.
	if offset < s.flushOffset {
		trim := s.flushOffset - offset
		if int64(len(buf)) <= trim {
			trim = int64(len(buf))
		}
		buf = buf[trim:]
	}
	if len(buf) == 0 && !flush && !finishWrite {
		// no need to send anything
		return nil, nil
	}

	req := bidiWriteObjectRequest(buf, offset, flush, finishWrite)
	if s.forceFirstMessage || reconnected {
		req.FirstMessage = &storagepb.BidiWriteObjectRequest_UploadId{UploadId: s.upid}
		s.forceFirstMessage = false
	}

	sendErr := s.stream.Send(req)
	if sendErr != nil {
		obj, err = drainInboundStream(s.stream)
		s.stream = nil
		if err == io.EOF {
			// This is unexpected - we got an error on Send(), but not on Recv().
			// Bubble up the sendErr.
			err = sendErr
		}
		return
	}

	if finishWrite {
		s.stream.CloseSend()
		obj, err = drainInboundStream(s.stream)
		s.stream = nil
		if err == io.EOF {
			err = nil
			if obj.GetSize() > s.flushOffset {
				s.progress(obj.GetSize())
			}
		}
		return
	}

	if flush {
		resp, err := s.stream.Recv()
		if err != nil {
			return nil, err
		}
		persistedOffset := resp.GetPersistedSize()
		if persistedOffset > s.flushOffset {
			s.flushOffset = persistedOffset
			s.progress(s.flushOffset)
		}
	}
	return
}

// uploadBuffer uploads the buffer at the given offset using a bi-directional
// Write stream. It will open a new stream if necessary (on the first call or
// after resuming from failure) and chunk the buffer per maxPerMessageWriteSize.
// The final Object is returned on success if doneReading is true.
//
// Returns object and any error that is not retriable.
func (w *gRPCWriter) uploadBuffer(recvd int, start int64, doneReading bool) (obj *storagepb.Object, err error) {
	if w.streamSender == nil {
		if w.append {
			// Appendable object semantics
			w.streamSender, err = w.newGRPCAppendBidiWriteBufferSender()
		} else if doneReading || w.forceOneShot {
			// One shot semantics
			w.streamSender, err = w.newGRPCOneshotBidiWriteBufferSender()
		} else {
			// Resumable write semantics
			w.streamSender, err = w.newGRPCResumableBidiWriteBufferSender()
		}
		if err != nil {
			return
		}
	}

	data := w.buf[:recvd]
	offset := start
	// We want to go through this loop at least once, in case we have to
	// finishWrite with an empty buffer.
	for {
		// Send as much as we can fit into a single gRPC message. Only flush once,
		// when sending the very last message.
		l := maxPerMessageWriteSize
		flush := false
		if len(data) <= l {
			l = len(data)
			flush = true
		}
		obj, err = w.streamSender.sendBuffer(data[:l], offset, flush, flush && doneReading)
		if err != nil {
			return nil, err
		}
		data = data[l:]
		offset += int64(l)
		if len(data) == 0 {
			break
		}
	}
	return
}

// read copies the data in the reader to the given buffer and reports how much
// data was read into the buffer and if there is no more data to read (EOF).
func (w *gRPCWriter) read() (int, bool, error) {
	// Set n to -1 to start the Read loop.
	var n, recvd int = -1, 0
	var err error
	for err == nil && n != 0 {
		// The routine blocks here until data is received.
		n, err = w.reader.Read(w.buf[recvd:])
		recvd += n
	}
	var done bool
	if err == io.EOF {
		done = true
		err = nil
	}
	return recvd, done, err
}

func checkCanceled(err error) error {
	if status.Code(err) == codes.Canceled {
		return context.Canceled
	}

	return err
}
