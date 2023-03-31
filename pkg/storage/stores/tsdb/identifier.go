package tsdb

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
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
	id, ok := parseSingleTenantTSDBPath(p)
	if ok {
		return newPrefixedIdentifier(id, filepath.Dir(p), ""), nil
	}

	multiID, ok := parseMultitenantTSDBPath(p)
	if !ok {
		return nil, fmt.Errorf("invalid tsdb path: %s", p)
	}

	parent := filepath.Dir(p)
	return newPrefixedIdentifier(multiID, parent, ""), nil
}

func newPrefixedIdentifier(id Identifier, path, name string) prefixedIdentifier {
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
	TS            time.Time
	From, Through model.Time
	Checksum      uint32
}

// str builds filename with format <file-creation-ts> + `-` + `compactor` + `-` + <oldest-chunk-start-ts> + `-` + <latest-chunk-end-ts> `-` + <index-checksum>
func (i SingleTenantTSDBIdentifier) str() string {
	return fmt.Sprintf(
		"%d-%s-%d-%d-%x.tsdb",
		i.TS.Unix(),
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

func parseSingleTenantTSDBPath(p string) (id SingleTenantTSDBIdentifier, ok bool) {
	// parsing as multitenant didn't work, so try single tenant

	// incorrect suffix
	trimmed := strings.TrimSuffix(p, ".tsdb")
	if trimmed == p {
		return
	}

	elems := strings.Split(trimmed, "-")
	if len(elems) != 5 {
		return
	}

	ts, err := strconv.Atoi(elems[0])
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

	checksum, err := strconv.ParseInt(elems[4], 16, 32)
	if err != nil {
		return
	}

	return SingleTenantTSDBIdentifier{
		TS:       time.Unix(int64(ts), 0),
		From:     model.Time(from),
		Through:  model.Time(through),
		Checksum: uint32(checksum),
	}, true

}

type MultitenantTSDBIdentifier struct {
	nodeName string
	ts       time.Time
}

// Name builds filename with format <file-creation-ts> + `-` + `<nodeName>
func (id MultitenantTSDBIdentifier) Name() string {
	return fmt.Sprintf("%d-%s.tsdb", id.ts.Unix(), id.nodeName)
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
		ts:       time.Unix(int64(ts), 0),
		nodeName: strings.Join(xs[1:], "-"),
	}, true
}
