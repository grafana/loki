package bloomshipper

import (
	"fmt"
	"hash"
	"hash/fnv"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

const (
	BloomPrefix  = "bloom"
	MetasPrefix  = "metas"
	BlocksPrefix = "blocks"

	extTarGz = ".tar.gz"
	extJSON  = ".json"
)

// KeyResolver is an interface for resolving keys to locations.
// This is used to determine where items are stored in object storage _and_ on disk.
// Using an interface allows us to abstract away platform specifics
// (e.g. OS path-specifics, object storage difference, etc)
// TODO(owen-d): implement resolvers that schema-aware, allowing us to change
// the locations of data across schema boundaries (for instance to upgrade|improve).
type KeyResolver interface {
	Meta(MetaRef) Location
	ParseMetaKey(Location) (MetaRef, error)
	Block(BlockRef) Location
	ParseBlockKey(Location) (BlockRef, error)
	Tenant(tenant, table string) Location
	TenantPrefix(loc Location) (string, error)
}

type defaultKeyResolver struct{}

func (defaultKeyResolver) Meta(ref MetaRef) Location {
	return simpleLocation{
		BloomPrefix,
		fmt.Sprintf("%v", ref.TableName),
		ref.TenantID,
		MetasPrefix,
		fmt.Sprintf("%v-%x%s", ref.Bounds, ref.Checksum, extJSON),
	}
}

func (defaultKeyResolver) ParseMetaKey(loc Location) (MetaRef, error) {
	dir, fn := path.Split(loc.Addr())
	fnParts := strings.Split(fn, "-")
	if len(fnParts) != 3 {
		return MetaRef{}, fmt.Errorf("failed to split filename parts of meta key %s : len must be 3, but was %d", loc, len(fnParts))
	}
	bounds, err := v1.ParseBoundsFromParts(fnParts[0], fnParts[1])
	if err != nil {
		return MetaRef{}, fmt.Errorf("failed to parse bounds of meta key %s : %w", loc, err)
	}
	withoutExt := strings.TrimSuffix(fnParts[2], extJSON)
	checksum, err := strconv.ParseUint(withoutExt, 16, 64)
	if err != nil {
		return MetaRef{}, fmt.Errorf("failed to parse checksum of meta key %s : %w", loc, err)
	}

	dirParts := strings.Split(path.Clean(dir), "/")
	if len(dirParts) < 4 {
		return MetaRef{}, fmt.Errorf("directory parts count must be 4 or greater, but was %d : [%s]", len(dirParts), loc)
	}

	return MetaRef{
		Ref: Ref{
			TenantID:  dirParts[len(dirParts)-2],
			TableName: dirParts[len(dirParts)-3],
			Bounds:    bounds,
			Checksum:  uint32(checksum),
		},
	}, nil
}

func (defaultKeyResolver) Block(ref BlockRef) Location {
	return simpleLocation{
		BloomPrefix,
		fmt.Sprintf("%v", ref.TableName),
		ref.TenantID,
		BlocksPrefix,
		ref.Bounds.String(),
		fmt.Sprintf("%d-%d-%x%s", ref.StartTimestamp, ref.EndTimestamp, ref.Checksum, extTarGz),
	}
}

func (defaultKeyResolver) ParseBlockKey(loc Location) (BlockRef, error) {
	dir, fn := path.Split(loc.Addr())
	fnParts := strings.Split(fn, "-")
	if len(fnParts) != 3 {
		return BlockRef{}, fmt.Errorf("failed to split filename parts of block key %s : len must be 3, but was %d", loc, len(fnParts))
	}
	interval, err := ParseIntervalFromParts(fnParts[0], fnParts[1])
	if err != nil {
		return BlockRef{}, fmt.Errorf("failed to parse bounds of meta key %s : %w", loc, err)
	}
	withoutExt := strings.TrimSuffix(fnParts[2], extTarGz)
	checksum, err := strconv.ParseUint(withoutExt, 16, 64)
	if err != nil {
		return BlockRef{}, fmt.Errorf("failed to parse checksum of meta key %s : %w", loc, err)
	}

	dirParts := strings.Split(path.Clean(dir), "/")
	if len(dirParts) < 5 {
		return BlockRef{}, fmt.Errorf("directory parts count must be 5 or greater, but was %d : [%s]", len(dirParts), loc)
	}

	bounds, err := v1.ParseBoundsFromAddr(dirParts[len(dirParts)-1])
	if err != nil {
		return BlockRef{}, fmt.Errorf("failed to parse bounds of block key %s : %w", loc, err)
	}

	return BlockRef{
		Ref: Ref{
			TenantID:       dirParts[len(dirParts)-3],
			TableName:      dirParts[len(dirParts)-4],
			Bounds:         bounds,
			StartTimestamp: interval.Start,
			EndTimestamp:   interval.End,
			Checksum:       uint32(checksum),
		},
	}, nil
}

func (defaultKeyResolver) Tenant(tenant, table string) Location {
	return simpleLocation{
		BloomPrefix,
		table,
		tenant,
	}
}

func (defaultKeyResolver) TenantPrefix(loc Location) (string, error) {
	dir, fn := path.Split(loc.Addr())

	dirParts := strings.Split(path.Clean(dir), "/")
	dirParts = append(dirParts, path.Clean(fn))
	if len(dirParts) < 3 {
		return "", fmt.Errorf("directory parts count must be 3 or greater, but was %d : [%s]", len(dirParts), loc)
	}

	// The tenant is the third part of the directory. E.g. bloom/schema_b_table_20088/1/metas where 1 is the tenant
	return dirParts[2], nil
}

type PrefixedResolver struct {
	prefix string
	KeyResolver
}

func NewPrefixedResolver(prefix string, resolver KeyResolver) KeyResolver {
	return PrefixedResolver{
		prefix:      prefix,
		KeyResolver: resolver,
	}
}

func (p PrefixedResolver) Meta(ref MetaRef) Location {
	return locations{
		key(p.prefix),
		p.KeyResolver.Meta(ref),
	}
}

func (p PrefixedResolver) Block(ref BlockRef) Location {
	return locations{
		key(p.prefix),
		p.KeyResolver.Block(ref),
	}
}

type hashable interface {
	Hash(hash.Hash32) error
}

type ShardedPrefixedResolver struct {
	prefixes []string
	KeyResolver
}

func NewShardedPrefixedResolver(prefixes []string, resolver KeyResolver) (KeyResolver, error) {
	n := len(prefixes)
	switch n {
	case 0:
		return nil, fmt.Errorf("requires at least 1 prefix")
	case 1:
		return NewPrefixedResolver(prefixes[0], resolver), nil
	default:
		return ShardedPrefixedResolver{
			prefixes:    prefixes,
			KeyResolver: resolver,
		}, nil
	}
}

func (r ShardedPrefixedResolver) prefix(ref hashable) key {
	h := fnv.New32()
	_ = ref.Hash(h)
	return key(r.prefixes[h.Sum32()%uint32(len(r.prefixes))])
}

func (r ShardedPrefixedResolver) Meta(ref MetaRef) Location {
	return locations{
		r.prefix(ref),
		r.KeyResolver.Meta(ref),
	}
}

func (r ShardedPrefixedResolver) Block(ref BlockRef) Location {
	return locations{
		r.prefix(ref),
		r.KeyResolver.Block(ref),
	}
}

type Location interface {
	Addr() string      // object storage location
	LocalPath() string // local path version
}

// simplest Location implementor, just a string
type key string

func (k key) Addr() string {
	return string(k)
}

func (k key) LocalPath() string {
	return string(k)
}

// simpleLocation is a simple implementation of Location combining multiple strings
type simpleLocation []string

func (xs simpleLocation) LocalPath() string {
	return filepath.Join(xs...)
}

func (xs simpleLocation) Addr() string {
	return path.Join(xs...)
}

// helper type for combining multiple locations into one
type locations []Location

func (ls locations) Addr() string {
	xs := make([]string, 0, len(ls))
	for _, l := range ls {
		xs = append(xs, l.Addr())
	}

	return path.Join(xs...)
}

func (ls locations) LocalPath() string {
	xs := make([]string, 0, len(ls))
	for _, l := range ls {
		xs = append(xs, l.LocalPath())
	}

	return filepath.Join(xs...)
}
