// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package objstore

import (
	"context"
	"io"
	"strings"
)

type PrefixedBucket struct {
	bkt    Bucket
	prefix string
}

func NewPrefixedBucket(bkt Bucket, prefix string) Bucket {
	if validPrefix(prefix) {
		return &PrefixedBucket{bkt: bkt, prefix: strings.Trim(prefix, DirDelim)}
	}

	return bkt
}

func validPrefix(prefix string) bool {
	prefix = strings.Replace(prefix, "/", "", -1)
	return len(prefix) > 0
}

func conditionalPrefix(prefix, name string) string {
	if len(name) > 0 {
		return withPrefix(prefix, name)
	}

	return name
}

func withPrefix(prefix, name string) string {
	return prefix + DirDelim + name
}

func (p *PrefixedBucket) Provider() ObjProvider { return p.bkt.Provider() }

func (p *PrefixedBucket) Close() error {
	return p.bkt.Close()
}

// Iter calls f for each entry in the given directory (not recursive.). The argument to f is the full
// object name including the prefix of the inspected directory.
// Entries are passed to function in sorted order.
func (p *PrefixedBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...IterOption) error {
	pdir := withPrefix(p.prefix, dir)

	return p.bkt.Iter(ctx, pdir, func(s string) error {
		return f(strings.TrimPrefix(s, p.prefix+DirDelim))
	}, options...)
}

func (p *PrefixedBucket) IterWithAttributes(ctx context.Context, dir string, f func(IterObjectAttributes) error, options ...IterOption) error {
	pdir := withPrefix(p.prefix, dir)

	return p.bkt.IterWithAttributes(ctx, pdir, func(attrs IterObjectAttributes) error {
		attrs.Name = strings.TrimPrefix(attrs.Name, p.prefix+DirDelim)
		return f(attrs)
	}, options...)
}

func (p *PrefixedBucket) SupportedIterOptions() []IterOptionType {
	return p.bkt.SupportedIterOptions()
}

// Get returns a reader for the given object name.
func (p *PrefixedBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return p.bkt.Get(ctx, conditionalPrefix(p.prefix, name))
}

// GetRange returns a new range reader for the given object name and range.
func (p *PrefixedBucket) GetRange(ctx context.Context, name string, off int64, length int64) (io.ReadCloser, error) {
	return p.bkt.GetRange(ctx, conditionalPrefix(p.prefix, name), off, length)
}

// Exists checks if the given object exists in the bucket.
func (p *PrefixedBucket) Exists(ctx context.Context, name string) (bool, error) {
	return p.bkt.Exists(ctx, conditionalPrefix(p.prefix, name))
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (p *PrefixedBucket) IsObjNotFoundErr(err error) bool {
	return p.bkt.IsObjNotFoundErr(err)
}

// IsAccessDeniedErr returns true if access to object is denied.
func (p *PrefixedBucket) IsAccessDeniedErr(err error) bool {
	return p.bkt.IsAccessDeniedErr(err)
}

// Attributes returns information about the specified object.
func (p *PrefixedBucket) Attributes(ctx context.Context, name string) (ObjectAttributes, error) {
	return p.bkt.Attributes(ctx, conditionalPrefix(p.prefix, name))
}

// Upload the contents of the reader as an object into the bucket.
// Upload should be idempotent.
func (p *PrefixedBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	return p.bkt.Upload(ctx, conditionalPrefix(p.prefix, name), r)
}

// Delete removes the object with the given name.
// If object does not exists in the moment of deletion, Delete should throw error.
func (p *PrefixedBucket) Delete(ctx context.Context, name string) error {
	return p.bkt.Delete(ctx, conditionalPrefix(p.prefix, name))
}

// Name returns the bucket name for the provider.
func (p *PrefixedBucket) Name() string {
	return p.bkt.Name()
}
