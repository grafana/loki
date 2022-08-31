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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	raw "google.golang.org/api/storage/v1"
	"google.golang.org/api/transport"
	htransport "google.golang.org/api/transport/http"
	iampb "google.golang.org/genproto/googleapis/iam/v1"
)

// httpStorageClient is the HTTP-JSON API implementation of the transport-agnostic
// storageClient interface.
//
// This is an experimental API and not intended for public use.
type httpStorageClient struct {
	creds    *google.Credentials
	hc       *http.Client
	readHost string
	raw      *raw.Service
	scheme   string
	settings *settings
}

// newHTTPStorageClient initializes a new storageClient that uses the HTTP-JSON
// Storage API.
//
// This is an experimental API and not intended for public use.
func newHTTPStorageClient(ctx context.Context, opts ...storageOption) (storageClient, error) {
	s := initSettings(opts...)
	o := s.clientOption

	var creds *google.Credentials
	// In general, it is recommended to use raw.NewService instead of htransport.NewClient
	// since raw.NewService configures the correct default endpoints when initializing the
	// internal http client. However, in our case, "NewRangeReader" in reader.go needs to
	// access the http client directly to make requests, so we create the client manually
	// here so it can be re-used by both reader.go and raw.NewService. This means we need to
	// manually configure the default endpoint options on the http client. Furthermore, we
	// need to account for STORAGE_EMULATOR_HOST override when setting the default endpoints.
	if host := os.Getenv("STORAGE_EMULATOR_HOST"); host == "" {
		// Prepend default options to avoid overriding options passed by the user.
		o = append([]option.ClientOption{option.WithScopes(ScopeFullControl, "https://www.googleapis.com/auth/cloud-platform"), option.WithUserAgent(userAgent)}, o...)

		o = append(o, internaloption.WithDefaultEndpoint("https://storage.googleapis.com/storage/v1/"))
		o = append(o, internaloption.WithDefaultMTLSEndpoint("https://storage.mtls.googleapis.com/storage/v1/"))

		// Don't error out here. The user may have passed in their own HTTP
		// client which does not auth with ADC or other common conventions.
		c, err := transport.Creds(ctx, o...)
		if err == nil {
			creds = c
			o = append(o, internaloption.WithCredentials(creds))
		}
	} else {
		var hostURL *url.URL

		if strings.Contains(host, "://") {
			h, err := url.Parse(host)
			if err != nil {
				return nil, err
			}
			hostURL = h
		} else {
			// Add scheme for user if not supplied in STORAGE_EMULATOR_HOST
			// URL is only parsed correctly if it has a scheme, so we build it ourselves
			hostURL = &url.URL{Scheme: "http", Host: host}
		}

		hostURL.Path = "storage/v1/"
		endpoint := hostURL.String()

		// Append the emulator host as default endpoint for the user
		o = append([]option.ClientOption{option.WithoutAuthentication()}, o...)

		o = append(o, internaloption.WithDefaultEndpoint(endpoint))
		o = append(o, internaloption.WithDefaultMTLSEndpoint(endpoint))
	}
	s.clientOption = o

	// htransport selects the correct endpoint among WithEndpoint (user override), WithDefaultEndpoint, and WithDefaultMTLSEndpoint.
	hc, ep, err := htransport.NewClient(ctx, s.clientOption...)
	if err != nil {
		return nil, fmt.Errorf("dialing: %v", err)
	}
	// RawService should be created with the chosen endpoint to take account of user override.
	rawService, err := raw.NewService(ctx, option.WithEndpoint(ep), option.WithHTTPClient(hc))
	if err != nil {
		return nil, fmt.Errorf("storage client: %v", err)
	}
	// Update readHost and scheme with the chosen endpoint.
	u, err := url.Parse(ep)
	if err != nil {
		return nil, fmt.Errorf("supplied endpoint %q is not valid: %v", ep, err)
	}

	return &httpStorageClient{
		creds:    creds,
		hc:       hc,
		readHost: u.Host,
		raw:      rawService,
		scheme:   u.Scheme,
		settings: s,
	}, nil
}

func (c *httpStorageClient) Close() error {
	c.hc.CloseIdleConnections()
	return nil
}

// Top-level methods.

func (c *httpStorageClient) GetServiceAccount(ctx context.Context, project string, opts ...storageOption) (string, error) {
	s := callSettings(c.settings, opts...)
	call := c.raw.Projects.ServiceAccount.Get(project)
	var res *raw.ServiceAccount
	err := run(ctx, func() error {
		var err error
		res, err = call.Context(ctx).Do()
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(call))
	if err != nil {
		return "", err
	}
	return res.EmailAddress, nil
}

func (c *httpStorageClient) CreateBucket(ctx context.Context, project string, attrs *BucketAttrs, opts ...storageOption) (*BucketAttrs, error) {
	s := callSettings(c.settings, opts...)
	var bkt *raw.Bucket
	if attrs != nil {
		bkt = attrs.toRawBucket()
	} else {
		bkt = &raw.Bucket{}
	}

	// If there is lifecycle information but no location, explicitly set
	// the location. This is a GCS quirk/bug.
	if bkt.Location == "" && bkt.Lifecycle != nil {
		bkt.Location = "US"
	}
	req := c.raw.Buckets.Insert(project, bkt)
	setClientHeader(req.Header())
	if attrs != nil && attrs.PredefinedACL != "" {
		req.PredefinedAcl(attrs.PredefinedACL)
	}
	if attrs != nil && attrs.PredefinedDefaultObjectACL != "" {
		req.PredefinedDefaultObjectAcl(attrs.PredefinedDefaultObjectACL)
	}
	var battrs *BucketAttrs
	err := run(ctx, func() error {
		b, err := req.Context(ctx).Do()
		if err != nil {
			return err
		}
		battrs, err = newBucket(b)
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(req))
	return battrs, err
}

func (c *httpStorageClient) ListBuckets(ctx context.Context, project string, opts ...storageOption) *BucketIterator {
	s := callSettings(c.settings, opts...)
	it := &BucketIterator{
		ctx:       ctx,
		projectID: project,
	}

	fetch := func(pageSize int, pageToken string) (token string, err error) {
		req := c.raw.Buckets.List(it.projectID)
		setClientHeader(req.Header())
		req.Projection("full")
		req.Prefix(it.Prefix)
		req.PageToken(pageToken)
		if pageSize > 0 {
			req.MaxResults(int64(pageSize))
		}
		var resp *raw.Buckets
		err = run(it.ctx, func() error {
			resp, err = req.Context(it.ctx).Do()
			return err
		}, s.retry, s.idempotent, setRetryHeaderHTTP(req))
		if err != nil {
			return "", err
		}
		for _, item := range resp.Items {
			b, err := newBucket(item)
			if err != nil {
				return "", err
			}
			it.buckets = append(it.buckets, b)
		}
		return resp.NextPageToken, nil
	}

	it.pageInfo, it.nextFunc = iterator.NewPageInfo(
		fetch,
		func() int { return len(it.buckets) },
		func() interface{} { b := it.buckets; it.buckets = nil; return b })

	return it
}

// Bucket methods.

func (c *httpStorageClient) DeleteBucket(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) error {
	s := callSettings(c.settings, opts...)
	req := c.raw.Buckets.Delete(bucket)
	setClientHeader(req.Header())
	if err := applyBucketConds("httpStorageClient.DeleteBucket", conds, req); err != nil {
		return err
	}
	if s.userProject != "" {
		req.UserProject(s.userProject)
	}

	return run(ctx, func() error { return req.Context(ctx).Do() }, s.retry, s.idempotent, setRetryHeaderHTTP(req))
}

func (c *httpStorageClient) GetBucket(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) (*BucketAttrs, error) {
	s := callSettings(c.settings, opts...)
	req := c.raw.Buckets.Get(bucket).Projection("full")
	setClientHeader(req.Header())
	err := applyBucketConds("httpStorageClient.GetBucket", conds, req)
	if err != nil {
		return nil, err
	}
	if s.userProject != "" {
		req.UserProject(s.userProject)
	}

	var resp *raw.Bucket
	err = run(ctx, func() error {
		resp, err = req.Context(ctx).Do()
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(req))

	var e *googleapi.Error
	if ok := errors.As(err, &e); ok && e.Code == http.StatusNotFound {
		return nil, ErrBucketNotExist
	}
	if err != nil {
		return nil, err
	}
	return newBucket(resp)
}
func (c *httpStorageClient) UpdateBucket(ctx context.Context, bucket string, uattrs *BucketAttrsToUpdate, conds *BucketConditions, opts ...storageOption) (*BucketAttrs, error) {
	s := callSettings(c.settings, opts...)
	rb := uattrs.toRawBucket()
	req := c.raw.Buckets.Patch(bucket, rb).Projection("full")
	setClientHeader(req.Header())
	err := applyBucketConds("httpStorageClient.UpdateBucket", conds, req)
	if err != nil {
		return nil, err
	}
	if s.userProject != "" {
		req.UserProject(s.userProject)
	}
	if uattrs != nil && uattrs.PredefinedACL != "" {
		req.PredefinedAcl(uattrs.PredefinedACL)
	}
	if uattrs != nil && uattrs.PredefinedDefaultObjectACL != "" {
		req.PredefinedDefaultObjectAcl(uattrs.PredefinedDefaultObjectACL)
	}

	var rawBucket *raw.Bucket
	err = run(ctx, func() error {
		rawBucket, err = req.Context(ctx).Do()
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(req))
	if err != nil {
		return nil, err
	}
	return newBucket(rawBucket)
}

func (c *httpStorageClient) LockBucketRetentionPolicy(ctx context.Context, bucket string, conds *BucketConditions, opts ...storageOption) error {
	s := callSettings(c.settings, opts...)

	var metageneration int64
	if conds != nil {
		metageneration = conds.MetagenerationMatch
	}
	req := c.raw.Buckets.LockRetentionPolicy(bucket, metageneration)

	return run(ctx, func() error {
		_, err := req.Context(ctx).Do()
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(req))
}
func (c *httpStorageClient) ListObjects(ctx context.Context, bucket string, q *Query, opts ...storageOption) *ObjectIterator {
	s := callSettings(c.settings, opts...)
	it := &ObjectIterator{
		ctx: ctx,
	}
	if q != nil {
		it.query = *q
	}
	fetch := func(pageSize int, pageToken string) (string, error) {
		req := c.raw.Objects.List(bucket)
		setClientHeader(req.Header())
		projection := it.query.Projection
		if projection == ProjectionDefault {
			projection = ProjectionFull
		}
		req.Projection(projection.String())
		req.Delimiter(it.query.Delimiter)
		req.Prefix(it.query.Prefix)
		req.StartOffset(it.query.StartOffset)
		req.EndOffset(it.query.EndOffset)
		req.Versions(it.query.Versions)
		req.IncludeTrailingDelimiter(it.query.IncludeTrailingDelimiter)
		if len(it.query.fieldSelection) > 0 {
			req.Fields("nextPageToken", googleapi.Field(it.query.fieldSelection))
		}
		req.PageToken(pageToken)
		if s.userProject != "" {
			req.UserProject(s.userProject)
		}
		if pageSize > 0 {
			req.MaxResults(int64(pageSize))
		}
		var resp *raw.Objects
		var err error
		err = run(it.ctx, func() error {
			resp, err = req.Context(it.ctx).Do()
			return err
		}, s.retry, s.idempotent, setRetryHeaderHTTP(req))
		if err != nil {
			var e *googleapi.Error
			if ok := errors.As(err, &e); ok && e.Code == http.StatusNotFound {
				err = ErrBucketNotExist
			}
			return "", err
		}
		for _, item := range resp.Items {
			it.items = append(it.items, newObject(item))
		}
		for _, prefix := range resp.Prefixes {
			it.items = append(it.items, &ObjectAttrs{Prefix: prefix})
		}
		return resp.NextPageToken, nil
	}
	it.pageInfo, it.nextFunc = iterator.NewPageInfo(
		fetch,
		func() int { return len(it.items) },
		func() interface{} { b := it.items; it.items = nil; return b })

	return it
}

// Object metadata methods.

func (c *httpStorageClient) DeleteObject(ctx context.Context, bucket, object string, conds *Conditions, opts ...storageOption) error {
	return errMethodNotSupported
}
func (c *httpStorageClient) GetObject(ctx context.Context, bucket, object string, conds *Conditions, opts ...storageOption) (*ObjectAttrs, error) {
	return nil, errMethodNotSupported
}
func (c *httpStorageClient) UpdateObject(ctx context.Context, bucket, object string, uattrs *ObjectAttrsToUpdate, conds *Conditions, opts ...storageOption) (*ObjectAttrs, error) {
	return nil, errMethodNotSupported
}

// Default Object ACL methods.

func (c *httpStorageClient) DeleteDefaultObjectACL(ctx context.Context, bucket string, entity ACLEntity, opts ...storageOption) error {
	s := callSettings(c.settings, opts...)
	req := c.raw.DefaultObjectAccessControls.Delete(bucket, string(entity))
	configureACLCall(ctx, s.userProject, req)
	return run(ctx, func() error { return req.Context(ctx).Do() }, s.retry, s.idempotent, setRetryHeaderHTTP(req))
}

func (c *httpStorageClient) ListDefaultObjectACLs(ctx context.Context, bucket string, opts ...storageOption) ([]ACLRule, error) {
	s := callSettings(c.settings, opts...)
	var acls *raw.ObjectAccessControls
	var err error
	req := c.raw.DefaultObjectAccessControls.List(bucket)
	configureACLCall(ctx, s.userProject, req)
	err = run(ctx, func() error {
		acls, err = req.Do()
		return err
	}, s.retry, true, setRetryHeaderHTTP(req))
	if err != nil {
		return nil, err
	}
	return toObjectACLRules(acls.Items), nil
}
func (c *httpStorageClient) UpdateDefaultObjectACL(ctx context.Context, opts ...storageOption) (*ACLRule, error) {
	return nil, errMethodNotSupported
}

// Bucket ACL methods.

func (c *httpStorageClient) DeleteBucketACL(ctx context.Context, bucket string, entity ACLEntity, opts ...storageOption) error {
	s := callSettings(c.settings, opts...)
	req := c.raw.BucketAccessControls.Delete(bucket, string(entity))
	configureACLCall(ctx, s.userProject, req)
	return run(ctx, func() error { return req.Context(ctx).Do() }, s.retry, s.idempotent, setRetryHeaderHTTP(req))
}

func (c *httpStorageClient) ListBucketACLs(ctx context.Context, bucket string, opts ...storageOption) ([]ACLRule, error) {
	s := callSettings(c.settings, opts...)
	var acls *raw.BucketAccessControls
	var err error
	req := c.raw.BucketAccessControls.List(bucket)
	configureACLCall(ctx, s.userProject, req)
	err = run(ctx, func() error {
		acls, err = req.Do()
		return err
	}, s.retry, true, setRetryHeaderHTTP(req))
	if err != nil {
		return nil, err
	}
	return toBucketACLRules(acls.Items), nil
}

func (c *httpStorageClient) UpdateBucketACL(ctx context.Context, bucket string, entity ACLEntity, role ACLRole, opts ...storageOption) (*ACLRule, error) {
	s := callSettings(c.settings, opts...)
	acl := &raw.BucketAccessControl{
		Bucket: bucket,
		Entity: string(entity),
		Role:   string(role),
	}
	req := c.raw.BucketAccessControls.Update(bucket, string(entity), acl)
	configureACLCall(ctx, s.userProject, req)
	var aclRule ACLRule
	var err error
	err = run(ctx, func() error {
		acl, err = req.Do()
		aclRule = toBucketACLRule(acl)
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(req))
	if err != nil {
		return nil, err
	}
	return &aclRule, nil
}

// configureACLCall sets the context, user project and headers on the apiary library call.
// This will panic if the call does not have the correct methods.
func configureACLCall(ctx context.Context, userProject string, call interface{ Header() http.Header }) {
	vc := reflect.ValueOf(call)
	vc.MethodByName("Context").Call([]reflect.Value{reflect.ValueOf(ctx)})
	if userProject != "" {
		vc.MethodByName("UserProject").Call([]reflect.Value{reflect.ValueOf(userProject)})
	}
	setClientHeader(call.Header())
}

// Object ACL methods.

func (c *httpStorageClient) DeleteObjectACL(ctx context.Context, bucket, object string, entity ACLEntity, opts ...storageOption) error {
	return errMethodNotSupported
}
func (c *httpStorageClient) ListObjectACLs(ctx context.Context, bucket, object string, opts ...storageOption) ([]ACLRule, error) {
	return nil, errMethodNotSupported
}
func (c *httpStorageClient) UpdateObjectACL(ctx context.Context, bucket, object string, entity ACLEntity, role ACLRole, opts ...storageOption) (*ACLRule, error) {
	return nil, errMethodNotSupported
}

// Media operations.

func (c *httpStorageClient) ComposeObject(ctx context.Context, req *composeObjectRequest, opts ...storageOption) (*ObjectAttrs, error) {
	return nil, errMethodNotSupported
}
func (c *httpStorageClient) RewriteObject(ctx context.Context, req *rewriteObjectRequest, opts ...storageOption) (*rewriteObjectResponse, error) {
	return nil, errMethodNotSupported
}

func (c *httpStorageClient) OpenReader(ctx context.Context, r *Reader, opts ...storageOption) error {
	return errMethodNotSupported
}
func (c *httpStorageClient) OpenWriter(ctx context.Context, w *Writer, opts ...storageOption) error {
	return errMethodNotSupported
}

// IAM methods.

func (c *httpStorageClient) GetIamPolicy(ctx context.Context, resource string, version int32, opts ...storageOption) (*iampb.Policy, error) {
	s := callSettings(c.settings, opts...)
	call := c.raw.Buckets.GetIamPolicy(resource).OptionsRequestedPolicyVersion(int64(version))
	setClientHeader(call.Header())
	if s.userProject != "" {
		call.UserProject(s.userProject)
	}
	var rp *raw.Policy
	err := run(ctx, func() error {
		var err error
		rp, err = call.Context(ctx).Do()
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(call))
	if err != nil {
		return nil, err
	}
	return iamFromStoragePolicy(rp), nil
}

func (c *httpStorageClient) SetIamPolicy(ctx context.Context, resource string, policy *iampb.Policy, opts ...storageOption) error {
	s := callSettings(c.settings, opts...)

	rp := iamToStoragePolicy(policy)
	call := c.raw.Buckets.SetIamPolicy(resource, rp)
	setClientHeader(call.Header())
	if s.userProject != "" {
		call.UserProject(s.userProject)
	}

	return run(ctx, func() error {
		_, err := call.Context(ctx).Do()
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(call))
}

func (c *httpStorageClient) TestIamPermissions(ctx context.Context, resource string, permissions []string, opts ...storageOption) ([]string, error) {
	s := callSettings(c.settings, opts...)
	call := c.raw.Buckets.TestIamPermissions(resource, permissions)
	setClientHeader(call.Header())
	if s.userProject != "" {
		call.UserProject(s.userProject)
	}
	var res *raw.TestIamPermissionsResponse
	err := run(ctx, func() error {
		var err error
		res, err = call.Context(ctx).Do()
		return err
	}, s.retry, s.idempotent, setRetryHeaderHTTP(call))
	if err != nil {
		return nil, err
	}
	return res.Permissions, nil
}

// HMAC Key methods.

func (c *httpStorageClient) GetHMACKey(ctx context.Context, desc *hmacKeyDesc, opts ...storageOption) (*HMACKey, error) {
	return nil, errMethodNotSupported
}
func (c *httpStorageClient) ListHMACKey(ctx context.Context, desc *hmacKeyDesc, opts ...storageOption) *HMACKeysIterator {
	return &HMACKeysIterator{}
}
func (c *httpStorageClient) UpdateHMACKey(ctx context.Context, desc *hmacKeyDesc, attrs *HMACKeyAttrsToUpdate, opts ...storageOption) (*HMACKey, error) {
	return nil, errMethodNotSupported
}
func (c *httpStorageClient) CreateHMACKey(ctx context.Context, desc *hmacKeyDesc, opts ...storageOption) (*HMACKey, error) {
	return nil, errMethodNotSupported
}
func (c *httpStorageClient) DeleteHMACKey(ctx context.Context, desc *hmacKeyDesc, opts ...storageOption) error {
	return errMethodNotSupported
}
