package bloomshipper

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

const (
	BloomPrefix  = "bloom"
	MetasPrefix  = "metas"
	BlocksPrefix = "blocks"
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
}

type defaultKeyResolver struct{}

func (defaultKeyResolver) Meta(ref MetaRef) Location {
	return simpleLocation{
		BloomPrefix,
		fmt.Sprintf("%v", ref.TableName),
		ref.TenantID,
		MetasPrefix,
		fmt.Sprintf("%v-%v", ref.Bounds, ref.Checksum),
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
	checksum, err := strconv.ParseUint(fnParts[2], 16, 64)
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
		fmt.Sprintf("%d-%d-%x", ref.StartTimestamp, ref.EndTimestamp, ref.Checksum),
	}
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
