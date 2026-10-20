package tsdb

import (
	"fmt"
	"hash"
	"math"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const compactedFileUploader = "compactor"

// Identifier can resolve an index to a name (in object storage)
// and a path (on disk)
type Identifier interface {
	Name() string
	Path() string
}

// identifierFromPath will detect whether this is a single or multitenant TSDB
func identifierFromPath(p string) (Identifier, error) {
	// try parsing as single tenant since the filename is more deterministic without an arbitrary nodename for uploader
	id, ok := ParseSingleTenantTSDBPath(p)
	if ok {
		return NewPrefixedIdentifier(id, filepath.Dir(p), ""), nil
	}

	multiID, ok := parseMultitenantTSDBPath(p)
	if !ok {
		return nil, fmt.Errorf("invalid tsdb path: %s", p)
	}

	parent := filepath.Dir(p)
	return NewPrefixedIdentifier(multiID, parent, ""), nil
}

func NewPrefixedIdentifier(id Identifier, path, name string) Identifier {
	return prefixedIdentifier{
		Identifier: id,
		parentPath: path,
		parentName: name,
	}
}

// parentIdentifier wraps an Identifier and prepends to its methods
type prefixedIdentifier struct {
	parentPath, parentName string
	Identifier
}

func (p prefixedIdentifier) Path() string {
	return filepath.Join(p.parentPath, p.Identifier.Path())
}

func (p prefixedIdentifier) Name() string {
	return path.Join(p.parentName, p.Identifier.Name())
}

// Identifier has all the information needed to resolve a TSDB index
// Notably this abstracts away OS path separators, etc.
type SingleTenantTSDBIdentifier struct {
	// ExportTSInSecs tells whether creation timestamp should be exported in unix seconds instead of nanoseconds.
	// timestamp in filename could be a unix second or a unix nanosecond so
	// helps us to be able to reproduce filename back from parsed identifier.
	// Should be true ideally for older files with creation timestamp in seconds.
	ExportTSInSecs bool
	TS             time.Time
	From, Through  model.Time
	Checksum       uint32
	Shard          index.ShardAnnotation
}

// IsSharded reports whether the file holds only one of several fingerprint
// ranges of its table.
func (i SingleTenantTSDBIdentifier) IsSharded() bool {
	return i.Shard.Of > 1
}

// FingerprintBounds returns the [from, through) fingerprint range the file
// holds. An unsharded file holds every fingerprint.
func (i SingleTenantTSDBIdentifier) FingerprintBounds() (model.Fingerprint, model.Fingerprint) {
	if !i.IsSharded() {
		return 0, model.Fingerprint(math.MaxUint64)
	}
	return i.Shard.GetFromThrough()
}

// ShardKey identifies the fingerprint range of the file, in the form used in
// file names: `<x>_of_<K>`. Unsharded files are `0_of_1`.
func (i SingleTenantTSDBIdentifier) ShardKey() string {
	if !i.IsSharded() {
		return index.NewShard(0, 1).String()
	}
	return i.Shard.String()
}

// implement Hash
func (i SingleTenantTSDBIdentifier) Hash(h hash.Hash32) (err error) {
	_, err = h.Write([]byte(i.str()))
	return errors.Wrap(err, "writing SingleTenantTSDBIdentifier")
}

// str builds filename with format <file-creation-ts> + `-` + `compactor` + `-` + <oldest-chunk-start-ts> + `-` + <latest-chunk-end-ts> `-` + <index-checksum>
// followed by `-` + <shard>_of_<factor> when the file is one of several fingerprint-range shards of its table.
func (i SingleTenantTSDBIdentifier) str() string {
	ts := int64(0)
	if i.ExportTSInSecs {
		ts = i.TS.Unix()
	} else {
		ts = i.TS.UnixNano()
	}
	if i.IsSharded() {
		return fmt.Sprintf(
			"%d-%s-%d-%d-%x-%s.tsdb",
			ts,
			compactedFileUploader,
			i.From,
			i.Through,
			i.Checksum,
			i.Shard.String(),
		)
	}
	return fmt.Sprintf(
		"%d-%s-%d-%d-%x.tsdb",
		ts,
		compactedFileUploader,
		i.From,
		i.Through,
		i.Checksum,
	)
}

func (i SingleTenantTSDBIdentifier) Name() string {
	return i.str()
}

func (i SingleTenantTSDBIdentifier) Path() string {
	return i.str()
}

func ParseSingleTenantTSDBPath(p string) (id SingleTenantTSDBIdentifier, ok bool) {
	// parsing as multitenant didn't work, so try single tenant

	// incorrect suffix
	trimmed := strings.TrimSuffix(p, ".tsdb")
	if trimmed == p {
		return
	}

	elems := strings.Split(trimmed, "-")
	if len(elems) != 5 && len(elems) != 6 {
		return
	}

	ts, err := strconv.ParseInt(elems[0], 10, 64)
	if err != nil {
		return
	}

	if elems[1] != compactedFileUploader {
		return
	}

	from, err := strconv.ParseInt(elems[2], 10, 64)
	if err != nil {
		return
	}

	through, err := strconv.ParseInt(elems[3], 10, 64)
	if err != nil {
		return
	}

	checksum, err := strconv.ParseUint(elems[4], 16, 32)
	if err != nil {
		return
	}

	var shard index.ShardAnnotation
	if len(elems) == 6 {
		shard, ok = parseShardSuffix(elems[5])
		if !ok {
			return id, false
		}
	}

	var parsedTS time.Time
	if len(elems[0]) <= 10 {
		parsedTS = time.Unix(ts, 0)
	} else {
		parsedTS = time.Unix(0, ts)
	}
	return SingleTenantTSDBIdentifier{
		ExportTSInSecs: len(elems[0]) <= 10,
		TS:             parsedTS,
		From:           model.Time(from),
		Through:        model.Time(through),
		Checksum:       uint32(checksum),
		Shard:          shard,
	}, true

}

// parseShardSuffix parses the `<x>_of_<K>` suffix of a sharded file name. K
// must be a power of two greater than 1 and x must be below K; `0_of_1` is
// accepted and normalised to the unsharded zero value.
func parseShardSuffix(s string) (index.ShardAnnotation, bool) {
	shard, err := index.ParseShardString(s)
	if err != nil {
		return index.ShardAnnotation{}, false
	}
	if shard.Of == 1 {
		// 0_of_1 is the whole table: the unsharded zero value
		return index.ShardAnnotation{}, true
	}
	return shard, true
}

type MultitenantTSDBIdentifier struct {
	NodeName string
	Ts       time.Time
}

// Name builds filename with format <file-creation-ts> + `-` + `<nodeName>
func (id MultitenantTSDBIdentifier) Name() string {
	return fmt.Sprintf("%d-%s.tsdb", id.Ts.Unix(), id.NodeName)
}

func (id MultitenantTSDBIdentifier) Path() string {
	// There are no directories, so reuse name
	return id.Name()
}

func parseMultitenantTSDBPath(p string) (id MultitenantTSDBIdentifier, ok bool) {
	cleaned := filepath.Base(p)
	return parseMultitenantTSDBNameFromBase(cleaned)
}

func parseMultitenantTSDBNameFromBase(name string) (res MultitenantTSDBIdentifier, ok bool) {

	trimmed := strings.TrimSuffix(name, ".tsdb")

	// incorrect suffix
	if trimmed == name {
		return
	}

	xs := strings.Split(trimmed, "-")
	if len(xs) < 2 {
		return
	}

	ts, err := strconv.Atoi(xs[0])
	if err != nil {
		return
	}

	return MultitenantTSDBIdentifier{
		Ts:       time.Unix(int64(ts), 0),
		NodeName: strings.Join(xs[1:], "-"),
	}, true
}
