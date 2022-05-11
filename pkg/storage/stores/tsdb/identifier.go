package tsdb

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

// Identifier can resolve an index to a name (in object storage)
// and a path (on disk)
type Identifier interface {
	Name() string
	Path() string
}

// identifierFromPath will detect whether this is a single or multitenant TSDB
func identifierFromPath(p string) (Identifier, error) {
	multiID, multitenant := parseMultitenantTSDBPath(p)
	if multitenant {
		parent := filepath.Dir(p)
		return newPrefixedIdentifier(multiID, parent, ""), nil
	}

	id, parent, ok := parseSingleTenantTSDBPath(p)
	if !ok {
		return nil, fmt.Errorf("invalid tsdb path: %s", p)
	}

	return newPrefixedIdentifier(id, parent, ""), nil
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

func newSuffixedIdentifier(id Identifier, pathSuffix string) suffixedIdentifier {
	return suffixedIdentifier{
		pathSuffix: pathSuffix,
		Identifier: id,
	}
}

// Generally useful for gzip extensions
type suffixedIdentifier struct {
	pathSuffix string
	Identifier
}

func (s suffixedIdentifier) Path() string {
	return s.Identifier.Path() + s.pathSuffix
}

// Identifier has all the information needed to resolve a TSDB index
// Notably this abstracts away OS path separators, etc.
type SingleTenantTSDBIdentifier struct {
	Tenant        string
	From, Through model.Time
	Checksum      uint32
}

func (i SingleTenantTSDBIdentifier) str() string {
	return fmt.Sprintf(
		"%s-%d-%d-%x.tsdb",
		index.IndexFilename,
		i.From,
		i.Through,
		i.Checksum,
	)
}

func (i SingleTenantTSDBIdentifier) Name() string {
	return path.Join(i.Tenant, i.str())
}

func (i SingleTenantTSDBIdentifier) Path() string {
	return filepath.Join(i.Tenant, i.str())
}

func parseSingleTenantTSDBPath(p string) (id SingleTenantTSDBIdentifier, parent string, ok bool) {
	// parsing as multitenant didn't work, so try single tenant
	file := filepath.Base(p)
	parents := filepath.Dir(p)
	pathPrefix := filepath.Dir(parents)
	tenant := filepath.Base(parents)

	// no tenant was provided
	if tenant == "." {
		return
	}

	// incorrect suffix
	trimmed := strings.TrimSuffix(file, ".tsdb")
	if trimmed == file {
		return
	}

	elems := strings.Split(trimmed, "-")
	if len(elems) != 4 {
		return
	}

	if elems[0] != index.IndexFilename {
		return
	}

	from, err := strconv.ParseInt(elems[1], 10, 64)
	if err != nil {
		return
	}

	through, err := strconv.ParseInt(elems[2], 10, 64)
	if err != nil {
		return
	}

	checksum, err := strconv.ParseInt(elems[3], 16, 32)
	if err != nil {
		return
	}

	return SingleTenantTSDBIdentifier{
		Tenant:   tenant,
		From:     model.Time(from),
		Through:  model.Time(through),
		Checksum: uint32(checksum),
	}, pathPrefix, true

}

type MultitenantTSDBIdentifier struct {
	nodeName string
	ts       time.Time
}

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

func parseMultitenantTSDBName(p string) (id MultitenantTSDBIdentifier, ok bool) {
	cleaned := path.Base(p)
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
