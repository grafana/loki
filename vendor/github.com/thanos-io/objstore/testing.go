// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package objstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/efficientgo/core/testutil"
)

func CreateTemporaryTestBucketName(t testing.TB) string {
	src := rand.NewSource(time.Now().UnixNano())

	// Bucket name need to conform: https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-s3-bucket-naming-requirements.html.
	name := strings.ReplaceAll(strings.Replace(fmt.Sprintf("test_%x_%s", src.Int63(), strings.ToLower(t.Name())), "_", "-", -1), "/", "-")
	if len(name) >= 63 {
		name = name[:63]
	}
	return name
}

// EmptyBucket deletes all objects from bucket. This operation is required to properly delete bucket as a whole.
// It is used for testing only.
// TODO(bplotka): Add retries.
func EmptyBucket(t testing.TB, ctx context.Context, bkt Bucket) {
	var wg sync.WaitGroup

	queue := []string{""}
	for len(queue) > 0 {
		elem := queue[0]
		queue = queue[1:]

		err := bkt.Iter(ctx, elem, func(p string) error {
			if strings.HasSuffix(p, DirDelim) {
				queue = append(queue, p)
				return nil
			}

			wg.Add(1)
			go func() {
				if err := bkt.Delete(ctx, p); err != nil {
					t.Logf("deleting object %s failed: %s", p, err)
				}
				wg.Done()
			}()
			return nil
		})
		if err != nil {
			t.Logf("iterating over bucket objects failed: %s", err)
			wg.Wait()
			return
		}
	}
	wg.Wait()
}

func WithNoopInstr(bkt Bucket) InstrumentedBucket {
	return noopInstrumentedBucket{Bucket: bkt}
}

type noopInstrumentedBucket struct {
	Bucket
}

func (b noopInstrumentedBucket) WithExpectedErrs(IsOpFailureExpectedFunc) Bucket {
	return b
}

func (b noopInstrumentedBucket) ReaderWithExpectedErrs(IsOpFailureExpectedFunc) BucketReader {
	return b
}

func AcceptanceTest(t *testing.T, bkt Bucket) {
	ctx := context.Background()

	_, err := bkt.Get(ctx, "")
	testutil.NotOk(t, err)
	testutil.Assert(t, !bkt.IsObjNotFoundErr(err), "expected user error got not found %s", err)

	_, err = bkt.Get(ctx, "id1/obj_1.some")
	testutil.NotOk(t, err)
	testutil.Assert(t, bkt.IsObjNotFoundErr(err), "expected not found error got %s", err)

	ok, err := bkt.Exists(ctx, "id1/obj_1.some")
	testutil.Ok(t, err)
	testutil.Assert(t, !ok, "expected not exits")

	_, err = bkt.Attributes(ctx, "id1/obj_1.some")
	testutil.NotOk(t, err)
	testutil.Assert(t, bkt.IsObjNotFoundErr(err), "expected not found error but got %s", err)

	// Upload first object.
	testutil.Ok(t, bkt.Upload(ctx, "id1/obj_1.some", strings.NewReader("@test-data@")))

	// Double check we can immediately read it.
	rc1, err := bkt.Get(ctx, "id1/obj_1.some")
	testutil.Ok(t, err)
	defer func() { testutil.Ok(t, rc1.Close()) }()

	sz, err := TryToGetSize(rc1)
	testutil.Ok(t, err)
	testutil.Equals(t, int64(11), sz, "expected size to be equal to 11")

	content, err := io.ReadAll(rc1)
	testutil.Ok(t, err)
	testutil.Equals(t, "@test-data@", string(content))

	// Check if we can get the correct size.
	attrs, err := bkt.Attributes(ctx, "id1/obj_1.some")
	testutil.Ok(t, err)
	testutil.Assert(t, attrs.Size == 11, "expected size to be equal to 11")

	rc2, err := bkt.GetRange(ctx, "id1/obj_1.some", 1, 3)
	testutil.Ok(t, err)
	defer func() { testutil.Ok(t, rc2.Close()) }()

	sz, err = TryToGetSize(rc2)
	testutil.Ok(t, err)
	testutil.Equals(t, int64(3), sz, "expected size to be equal to 3")

	content, err = io.ReadAll(rc2)
	testutil.Ok(t, err)
	testutil.Equals(t, "tes", string(content))

	// Unspecified range with offset.
	rcUnspecifiedLen, err := bkt.GetRange(ctx, "id1/obj_1.some", 1, -1)
	testutil.Ok(t, err)
	defer func() { testutil.Ok(t, rcUnspecifiedLen.Close()) }()

	sz, err = TryToGetSize(rcUnspecifiedLen)
	testutil.Ok(t, err)
	testutil.Equals(t, int64(10), sz, "expected size to be equal to 10")

	content, err = io.ReadAll(rcUnspecifiedLen)
	testutil.Ok(t, err)
	testutil.Equals(t, "test-data@", string(content))

	// Out of band offset. Do not rely on outcome.
	// NOTE: For various providers we have different outcome.
	// * GCS is giving 416 status code
	// * S3 errors immdiately with invalid range error.
	// * inmem and filesystem are returning 0 bytes.
	//rcOffset, err := bkt.GetRange(ctx, "id1/obj_1.some", 124141, 3)

	// Out of band length. We expect to read file fully.
	rcLength, err := bkt.GetRange(ctx, "id1/obj_1.some", 3, 9999)
	testutil.Ok(t, err)
	defer func() { testutil.Ok(t, rcLength.Close()) }()

	sz, err = TryToGetSize(rcLength)
	testutil.Ok(t, err)
	testutil.Equals(t, int64(8), sz, "expected size to be equal to 8")

	content, err = io.ReadAll(rcLength)
	testutil.Ok(t, err)
	testutil.Equals(t, "st-data@", string(content))

	ok, err = bkt.Exists(ctx, "id1/obj_1.some")
	testutil.Ok(t, err)
	testutil.Assert(t, ok, "expected exits")

	// Upload other objects.
	testutil.Ok(t, bkt.Upload(ctx, "id1/obj_2.some", strings.NewReader("@test-data2@")))
	// Upload should be idempotent.
	testutil.Ok(t, bkt.Upload(ctx, "id1/obj_2.some", strings.NewReader("@test-data2@")))
	testutil.Ok(t, bkt.Upload(ctx, "id1/obj_3.some", strings.NewReader("@test-data3@")))
	testutil.Ok(t, bkt.Upload(ctx, "id1/sub/subobj_1.some", strings.NewReader("@test-data4@")))
	testutil.Ok(t, bkt.Upload(ctx, "id1/sub/subobj_2.some", strings.NewReader("@test-data5@")))
	testutil.Ok(t, bkt.Upload(ctx, "id2/obj_4.some", strings.NewReader("@test-data6@")))
	testutil.Ok(t, bkt.Upload(ctx, "obj_5.some", strings.NewReader("@test-data7@")))

	// Can we iter over items from top dir?
	var seen []string
	testutil.Ok(t, bkt.Iter(ctx, "", func(fn string) error {
		seen = append(seen, fn)
		return nil
	}))
	expected := []string{"obj_5.some", "id1/", "id2/"}
	sort.Strings(expected)
	sort.Strings(seen)
	testutil.Equals(t, expected, seen)

	// Can we iter over items from top dir recursively?
	seen = []string{}
	testutil.Ok(t, bkt.Iter(ctx, "", func(fn string) error {
		seen = append(seen, fn)
		return nil
	}, WithRecursiveIter()))
	expected = []string{"id1/obj_1.some", "id1/obj_2.some", "id1/obj_3.some", "id1/sub/subobj_1.some", "id1/sub/subobj_2.some", "id2/obj_4.some", "obj_5.some"}
	sort.Strings(expected)
	sort.Strings(seen)
	testutil.Equals(t, expected, seen)

	// Can we iter over items from id1/ dir?
	seen = []string{}
	testutil.Ok(t, bkt.Iter(ctx, "id1/", func(fn string) error {
		seen = append(seen, fn)
		return nil
	}))
	testutil.Equals(t, []string{"id1/obj_1.some", "id1/obj_2.some", "id1/obj_3.some", "id1/sub/"}, seen)

	// Can we iter over items from id1/ dir recursively?
	seen = []string{}
	testutil.Ok(t, bkt.Iter(ctx, "id1/", func(fn string) error {
		seen = append(seen, fn)
		return nil
	}, WithRecursiveIter()))
	testutil.Equals(t, []string{"id1/obj_1.some", "id1/obj_2.some", "id1/obj_3.some", "id1/sub/subobj_1.some", "id1/sub/subobj_2.some"}, seen)

	// Can we iter over items from id1 dir?
	seen = []string{}
	testutil.Ok(t, bkt.Iter(ctx, "id1", func(fn string) error {
		seen = append(seen, fn)
		return nil
	}))
	testutil.Equals(t, []string{"id1/obj_1.some", "id1/obj_2.some", "id1/obj_3.some", "id1/sub/"}, seen)

	// Can we iter over items from id1 dir recursively?
	seen = []string{}
	testutil.Ok(t, bkt.Iter(ctx, "id1", func(fn string) error {
		seen = append(seen, fn)
		return nil
	}, WithRecursiveIter()))
	testutil.Equals(t, []string{"id1/obj_1.some", "id1/obj_2.some", "id1/obj_3.some", "id1/sub/subobj_1.some", "id1/sub/subobj_2.some"}, seen)

	// Can we iter over items from not existing dir?
	testutil.Ok(t, bkt.Iter(ctx, "id0", func(fn string) error {
		t.Error("Not expected to loop through not existing directory")
		t.FailNow()

		return nil
	}))

	testutil.Ok(t, bkt.Delete(ctx, "id1/obj_2.some"))

	// Delete is expected to fail on non existing object.
	// NOTE: Don't rely on this. S3 is not complying with this as GCS is.
	// testutil.NotOk(t, bkt.Delete(ctx, "id1/obj_2.some"))

	// Can we iter over items from id1/ dir and see obj2 being deleted?
	seen = []string{}
	testutil.Ok(t, bkt.Iter(ctx, "id1/", func(fn string) error {
		seen = append(seen, fn)
		return nil
	}))
	testutil.Equals(t, []string{"id1/obj_1.some", "id1/obj_3.some", "id1/sub/"}, seen)

	testutil.Ok(t, bkt.Delete(ctx, "id2/obj_4.some"))

	seen = []string{}
	testutil.Ok(t, bkt.Iter(ctx, "", func(fn string) error {
		seen = append(seen, fn)
		return nil
	}))
	expected = []string{"obj_5.some", "id1/"}
	sort.Strings(expected)
	sort.Strings(seen)
	testutil.Equals(t, expected, seen)

	testutil.Ok(t, bkt.Upload(ctx, "obj_6.som", bytes.NewReader(make([]byte, 1024*1024*200))))
	testutil.Ok(t, bkt.Delete(ctx, "obj_6.som"))
}

type delayingBucket struct {
	bkt   Bucket
	delay time.Duration
}

func WithDelay(bkt Bucket, delay time.Duration) Bucket {
	return &delayingBucket{bkt: bkt, delay: delay}
}

func (d *delayingBucket) Provider() ObjProvider { return d.bkt.Provider() }

func (d *delayingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	time.Sleep(d.delay)
	return d.bkt.Get(ctx, name)
}

func (d *delayingBucket) Attributes(ctx context.Context, name string) (ObjectAttributes, error) {
	time.Sleep(d.delay)
	return d.bkt.Attributes(ctx, name)
}

func (d *delayingBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...IterOption) error {
	time.Sleep(d.delay)
	return d.bkt.Iter(ctx, dir, f, options...)
}

func (d *delayingBucket) IterWithAttributes(ctx context.Context, dir string, f func(IterObjectAttributes) error, options ...IterOption) error {
	time.Sleep(d.delay)
	return d.bkt.IterWithAttributes(ctx, dir, f, options...)
}

func (d *delayingBucket) SupportedIterOptions() []IterOptionType {
	return d.bkt.SupportedIterOptions()
}

func (d *delayingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	time.Sleep(d.delay)
	return d.bkt.GetRange(ctx, name, off, length)
}

func (d *delayingBucket) Exists(ctx context.Context, name string) (bool, error) {
	time.Sleep(d.delay)
	return d.bkt.Exists(ctx, name)
}

func (d *delayingBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	time.Sleep(d.delay)
	return d.bkt.Upload(ctx, name, r)
}

func (d *delayingBucket) Delete(ctx context.Context, name string) error {
	time.Sleep(d.delay)
	return d.bkt.Delete(ctx, name)
}

func (d *delayingBucket) Name() string {
	time.Sleep(d.delay)
	return d.bkt.Name()
}

func (d *delayingBucket) Close() error {
	// No delay for a local operation.
	return d.bkt.Close()
}
func (d *delayingBucket) IsObjNotFoundErr(err error) bool {
	// No delay for a local operation.
	return d.bkt.IsObjNotFoundErr(err)
}

func (d *delayingBucket) IsAccessDeniedErr(err error) bool {
	return d.bkt.IsAccessDeniedErr(err)
}
