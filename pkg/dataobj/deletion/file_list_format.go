package deletion

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"time"

	"github.com/thanos-io/objstore"
)

// ErrMalformedFileList means that a stored file list does not follow the
// stored format, holds another tenant, or has an unknown version. Retrying
// does not fix it.
var ErrMalformedFileList = errors.New("malformed file list")

// fileListVersion is the version of the stored format that this package
// writes and reads.
const fileListVersion = 1

// fileListHeader is the first record of a stored file list.
//
// A stored file list is one gzip stream of JSON Lines. The first record is
// the header. Each later record holds one path. The paths are in strictly
// increasing order, so that a reader can merge them with a sorted bucket
// listing. For example:
//
//	{"version":1,"tenant":"a","created_at":"2026-11-04T12:00:00Z"}
//	{"path":"index/v0/indexes/00/x"}
//	{"path":"objects/tenants/a/aa/y"}
//
// Decoding rejects unknown fields and other versions. The writer reads only
// the header, so a change to the format must change the version. Then older
// writers do not replace a list that they cannot read.
type fileListHeader struct {
	Version   int       `json:"version"`
	Tenant    string    `json:"tenant"`
	CreatedAt time.Time `json:"created_at"`
}

func (h fileListHeader) validate() error {
	if h.Version != fileListVersion {
		return fmt.Errorf("unsupported version %d, want %d", h.Version, fileListVersion)
	}
	if err := validateTenant(h.Tenant); err != nil {
		return err
	}
	if h.CreatedAt.IsZero() {
		return errors.New("creation time is not set")
	}
	return nil
}

type pathRecord struct {
	Path string `json:"path"`
}

func fileListPath(tenant string) string {
	return "deletion/" + tenant + "/files.jsonl.gz"
}

// encodeFileList returns the stored form of list. list must be valid.
func encodeFileList(list FileList) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(list.header()); err != nil {
		return nil, err
	}
	for _, p := range list.Paths {
		if err := enc.Encode(pathRecord{Path: p}); err != nil {
			return nil, err
		}
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fileListDecoder reads a stored file list. It reads the header when it is
// created, and each path on demand.
//
// The errors of fileListDecoder wrap ErrMalformedFileList if the stored list
// is malformed. They do not wrap it if a read of the source failed, because a
// retry can fix that.
type fileListDecoder struct {
	src    *readErrRecorder
	buf    *bufio.Reader
	dec    *json.Decoder
	header fileListHeader
	last   string
	// err is the first error of next, including io.EOF. The JSON decoder
	// already consumed a record that fails validation. So a later call that
	// went on would skip that record.
	err error
}

// newFileListDecoder reads the header of the stored file list in r, and
// checks that the list holds tenant.
func newFileListDecoder(r io.Reader, tenant string) (*fileListDecoder, error) {
	src := &readErrRecorder{r: r}
	// gzip reads directly from an io.ByteReader. This keeps the bytes after
	// the gzip stream in buf, so that checkEnd can find them.
	buf := bufio.NewReader(src)
	gz, err := gzip.NewReader(buf)
	if err != nil {
		return nil, src.wrapErr("opening gzip stream", err)
	}
	// A second gzip stream is not part of the format. Without this, gzip
	// reads it as more of the list.
	gz.Multistream(false)
	dec := json.NewDecoder(gz)
	dec.DisallowUnknownFields()

	var header fileListHeader
	if err := dec.Decode(&header); err != nil {
		return nil, src.wrapErr("decoding header", err)
	}
	if err := header.validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid header: %w", ErrMalformedFileList, err)
	}
	if header.Tenant != tenant {
		return nil, fmt.Errorf("%w: header holds tenant %q, want %q", ErrMalformedFileList, header.Tenant, tenant)
	}
	return &fileListDecoder{src: src, buf: buf, dec: dec, header: header}, nil
}

// next returns the next path. It returns io.EOF after the last path. After
// an error, it returns the same error on each call.
func (d *fileListDecoder) next() (string, error) {
	if d.err != nil {
		return "", d.err
	}
	p, err := d.decodePath()
	if err != nil {
		d.err = err
	}
	return p, err
}

func (d *fileListDecoder) decodePath() (string, error) {
	var rec pathRecord
	err := d.dec.Decode(&rec)
	if err == io.EOF {
		return "", d.checkEnd()
	}
	if err != nil {
		return "", d.src.wrapErr("decoding path", err)
	}
	if rec.Path == "" {
		return "", fmt.Errorf("%w: empty path after %q", ErrMalformedFileList, d.last)
	}
	if rec.Path <= d.last {
		return "", fmt.Errorf("%w: path %q is not after %q", ErrMalformedFileList, rec.Path, d.last)
	}
	d.last = rec.Path
	return rec.Path, nil
}

// checkEnd returns io.EOF if the source holds no data after the gzip stream.
func (d *fileListDecoder) checkEnd() error {
	_, err := d.buf.ReadByte()
	switch {
	case err == io.EOF:
		return io.EOF
	case err != nil:
		return d.src.wrapErr("checking the end of the stream", err)
	default:
		return fmt.Errorf("%w: data after the end of the gzip stream", ErrMalformedFileList)
	}
}

// readErrRecorder records the first error of r other than io.EOF.
type readErrRecorder struct {
	r   io.Reader
	err error
}

func (r *readErrRecorder) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err != nil && err != io.EOF && r.err == nil {
		r.err = err
	}
	return n, err
}

// wrapErr returns err with context. The result wraps ErrMalformedFileList unless
// a read of r failed.
func (r *readErrRecorder) wrapErr(step string, err error) error {
	if r.err != nil {
		return fmt.Errorf("reading stored file list: %s: %w", step, err)
	}
	return fmt.Errorf("%w: %s: %w", ErrMalformedFileList, step, err)
}

// FileListReader reads a stored file list as a stream.
//
// FileListReader is not safe for concurrent use.
type FileListReader struct {
	rc  io.ReadCloser
	dec *fileListDecoder
}

// OpenFileList opens the stored file list of tenant. bucket is the root of the
// data object storage. The caller must close the returned reader.
//
// If bucket fails to get the list, OpenFileList returns the error of bucket
// unchanged, so that bucket.IsObjNotFoundErr detects a missing list. If the
// header is malformed, holds another tenant, or has an unknown version, the
// error wraps ErrMalformedFileList.
func OpenFileList(ctx context.Context, bucket objstore.BucketReader, tenant string) (*FileListReader, error) {
	if err := validateTenant(tenant); err != nil {
		return nil, err
	}
	p := fileListPath(tenant)
	rc, err := bucket.Get(ctx, p)
	if err != nil {
		return nil, err
	}
	dec, err := newFileListDecoder(rc, tenant)
	if err != nil {
		rc.Close()
		return nil, fmt.Errorf("opening file list %s: %w", p, err)
	}
	return &FileListReader{rc: rc, dec: dec}, nil
}

// Tenant returns the tenant of the list.
func (r *FileListReader) Tenant() string { return r.dec.header.Tenant }

// CreatedAt returns the creation time of the list.
func (r *FileListReader) CreatedAt() time.Time { return r.dec.header.CreatedAt }

// Paths returns an iterator over the paths of the list, in strictly
// increasing order.
//
// The iterator yields an error and stops if a read fails or the list is
// malformed. If the list is malformed, the error wraps ErrMalformedFileList.
//
// gzip verifies the checksum of the list only at its end. So a corrupt list
// can yield wrong paths before the error. The caller must not act on any
// path until the iteration ends without an error.
//
// The iterator resumes after the last path that it yielded. After an error,
// it yields the same error again.
func (r *FileListReader) Paths() iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for {
			p, err := r.dec.next()
			if err == io.EOF {
				return
			}
			if err != nil {
				yield("", err)
				return
			}
			if !yield(p, nil) {
				return
			}
		}
	}
}

// Close closes the underlying object reader.
func (r *FileListReader) Close() error { return r.rc.Close() }
