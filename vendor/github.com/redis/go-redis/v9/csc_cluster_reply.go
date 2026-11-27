package redis

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9/internal/proto"
)

// A missing key can be created on an importing node without invalidating the
// source node's tracked absence. Cluster caches therefore retain only replies
// that establish the existence of every Redis key they depend on. Ambiguous
// replies are still returned to callers, but are not published to the cache.
type clusterCSCReplyPolicy uint8

const (
	clusterCSCReplyBypass clusterCSCReplyPolicy = iota
	clusterCSCReplyPresent
	clusterCSCReplyNonEmpty
	clusterCSCReplyPositive
	clusterCSCReplyAnyPresent
	clusterCSCReplyMembers
	clusterCSCReplyAllPresent
	clusterCSCReplyExists
	clusterCSCReplyType
)

func clusterCSCCommandPolicy(name string) clusterCSCReplyPolicy {
	switch name {
	case "get", "hget", "lindex", "lpos", "zscore", "zrank", "zrevrank",
		"geodist", "json.get", "ts.get", "ts.info", "ts.range", "ts.revrange":
		return clusterCSCReplyPresent
	case "getrange", "substr", "hgetall", "hkeys", "hvals", "lrange", "smembers",
		"sinter", "zinter", "zrange", "zrangebylex", "zrangebyscore", "zrevrange",
		"zrevrangebylex", "zrevrangebyscore", "sort_ro", "geosearch",
		"georadiusbymember_ro", "georadius_ro", "xrange", "xrevrange",
		"bitrangeintarray":
		return clusterCSCReplyNonEmpty
	case "getbit", "strlen", "hlen", "hexists", "hstrlen", "llen", "scard",
		"sismember", "sintercard", "zcard", "zcount", "zlexcount", "bitcount", "xlen":
		return clusterCSCReplyPositive
	case "hmget", "zmscore", "geohash", "geopos":
		// These commands read several members/fields of ONE Redis key.
		return clusterCSCReplyAnyPresent
	case "smismember":
		return clusterCSCReplyMembers
	case "mget", "json.mget":
		return clusterCSCReplyAllPresent
	case "exists":
		return clusterCSCReplyExists
	case "type":
		return clusterCSCReplyType
	default:
		// In particular, nonempty unions/differences do not prove that all
		// inputs exist. Do not add EXISTS probes or infer presence from them.
		return clusterCSCReplyBypass
	}
}

// clusterCSCReplyCacheable runs only on publication, never on hits. Decode just
// the command header from the immutable cache key: background work must not read
// a caller's possibly abandoned/reused Cmder. No second copy of the command or
// reply payload is needed. Protocol validity is checked by the normal readers;
// this additional check decides whether the successful reply proves presence.
func (c *baseClient) clusterCSCReplyCacheable(key string, raw []byte, view *commandMetadataView) bool {
	wire, ok := strings.CutPrefix(key, c.cscKeyPrefix)
	if !ok || c.cscKeyPrefix == "" {
		return false
	}
	wire, ok = strings.CutPrefix(wire, view.cscFingerprint)
	if !ok {
		return false
	}
	wire, ok = strings.CutPrefix(wire, cscNamespaceSep)
	if !ok {
		return false
	}
	r := proto.NewReaderSize(strings.NewReader(wire), 128)
	argc, err := r.ReadArrayLen()
	if err != nil || argc < 2 {
		return false
	}
	name, err := r.ReadString()
	if err != nil {
		return false
	}
	name = strings.ToLower(name)
	keys := argc - 1
	if name == "json.mget" {
		keys-- // The last argument is a JSON path, not a Redis key.
	}
	return clusterCSCCommandPolicy(name).accepts(raw, keys)
}

func (p clusterCSCReplyPolicy) accepts(raw []byte, keys int) bool {
	if p == clusterCSCReplyBypass || len(raw) == 0 {
		return false
	}
	r := proto.NewReaderSize(bytes.NewReader(raw), 128)
	if p == clusterCSCReplyType {
		s, err := r.ReadString()
		return err == nil && s != "" && s != "none"
	}
	// ReadLine skips RESP3 attributes and rejects errors and all null forms.
	line, err := r.ReadLine()
	if err != nil {
		return false
	}
	switch p {
	case clusterCSCReplyPresent:
		// LPOS COUNT can return an empty array for a missing list. Other
		// allowed commands may return aggregates too, so conservatively
		// reject empty aggregates, while preserving GET's empty string.
		switch line[0] {
		case proto.RespArray, proto.RespSet, proto.RespMap:
			n, err := strconv.ParseInt(string(line[1:]), 10, 64)
			return err == nil && n > 0
		case proto.RespString:
			n, err := strconv.ParseInt(string(line[1:]), 10, 64)
			return err == nil && n >= 0
		case proto.RespInt, proto.RespFloat:
			return true // Zero is valid, e.g. ZRANK of an existing member.
		}
	case clusterCSCReplyNonEmpty, clusterCSCReplyPositive, clusterCSCReplyExists:
		if p == clusterCSCReplyNonEmpty {
			if line[0] != proto.RespString && line[0] != proto.RespArray &&
				line[0] != proto.RespSet && line[0] != proto.RespMap {
				return false
			}
		} else if line[0] != proto.RespInt {
			return false
		}
		n, err := strconv.ParseInt(string(line[1:]), 10, 64)
		if p == clusterCSCReplyExists {
			// EXISTS counts duplicate key arguments separately too.
			return err == nil && keys > 0 && n == int64(keys)
		}
		return err == nil && n > 0
	case clusterCSCReplyAnyPresent, clusterCSCReplyMembers, clusterCSCReplyAllPresent:
		if line[0] != proto.RespArray {
			return false
		}
		n, err := strconv.Atoi(string(line[1:]))
		if err != nil || n <= 0 || (p == clusterCSCReplyAllPresent && n != keys) {
			return false
		}
		for i := 0; i < n; i++ {
			line, err := r.ReadLine()
			if err == Nil && p == clusterCSCReplyAnyPresent {
				continue
			}
			if err != nil {
				return false
			}
			switch p {
			case clusterCSCReplyMembers:
				if line[0] != proto.RespInt {
					return false
				}
				n, err := strconv.ParseInt(string(line[1:]), 10, 64)
				if err != nil {
					return false
				}
				if n == 1 {
					return true
				}
			case clusterCSCReplyAnyPresent:
				return true
			case clusterCSCReplyAllPresent:
				// MGET/JSON.MGET return a string (possibly empty) for every
				// present input; even one null rejects the whole cache entry.
				if line[0] != proto.RespString || r.Discard(line) != nil {
					return false
				}
			}
		}
		return p == clusterCSCReplyAllPresent
	}
	return false
}
