// Commands from https://redis.io/commands#hash

package miniredis

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/alicebob/miniredis/v2/server"
)

// commandsHash handles all hash value operations.
func commandsHash(m *Miniredis) {
	m.srv.Register("HDEL", m.cmdHdel)
	m.srv.Register("HEXISTS", m.cmdHexists, server.ReadOnlyOption())
	m.srv.Register("HGET", m.cmdHget, server.ReadOnlyOption())
	m.srv.Register("HGETALL", m.cmdHgetall, server.ReadOnlyOption())
	m.srv.Register("HINCRBY", m.cmdHincrby)
	m.srv.Register("HINCRBYFLOAT", m.cmdHincrbyfloat)
	m.srv.Register("HKEYS", m.cmdHkeys, server.ReadOnlyOption())
	m.srv.Register("HLEN", m.cmdHlen, server.ReadOnlyOption())
	m.srv.Register("HMGET", m.cmdHmget, server.ReadOnlyOption())
	m.srv.Register("HMSET", m.cmdHmset)
	m.srv.Register("HSET", m.cmdHset)
	m.srv.Register("HSETNX", m.cmdHsetnx)
	m.srv.Register("HSTRLEN", m.cmdHstrlen, server.ReadOnlyOption())
	m.srv.Register("HVALS", m.cmdHvals, server.ReadOnlyOption())
	m.srv.Register("HSCAN", m.cmdHscan, server.ReadOnlyOption())
	m.srv.Register("HRANDFIELD", m.cmdHrandfield, server.ReadOnlyOption())
	m.srv.Register("HEXPIRE", m.cmdHexpire)
	m.srv.Register("HPERSIST", m.cmdHpersist)
	m.srv.Register("HTTL", m.cmdHttl, server.ReadOnlyOption())
	m.srv.Register("HPTTL", m.cmdHpttl, server.ReadOnlyOption())
	m.srv.Register("HSETEX", m.cmdHsetex)
}

// HSET
func (m *Miniredis) cmdHset(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(3)) {
		return
	}

	key, pairs := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if len(pairs)%2 == 1 {
			c.WriteError(errWrongNumber(cmd))
			return
		}

		if t, ok := db.keys[key]; ok && t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		new := db.hashSet(key, pairs...)
		c.WriteInt(new)
	})
}

// HSETNX
func (m *Miniredis) cmdHsetnx(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(3)) {
		return
	}

	opts := struct {
		key   string
		field string
		value string
	}{
		key:   args[0],
		field: args[1],
		value: args[2],
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[opts.key]; ok && t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		if _, ok := db.hashKeys[opts.key]; !ok {
			db.hashKeys[opts.key] = map[string]string{}
			db.keys[opts.key] = keyTypeHash
		}
		_, ok := db.hashKeys[opts.key][opts.field]
		if ok {
			c.WriteInt(0)
			return
		}
		db.hashKeys[opts.key][opts.field] = opts.value
		db.incr(opts.key)
		c.WriteInt(1)
	})
}

// HMSET
func (m *Miniredis) cmdHmset(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(3)) {
		return
	}

	key, args := args[0], args[1:]
	if len(args)%2 != 0 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		for len(args) > 0 {
			field, value := args[0], args[1]
			args = args[2:]
			db.hashSet(key, field, value)
		}
		c.WriteOK()
	})
}

// HGET
func (m *Miniredis) cmdHget(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(2)) {
		return
	}

	key, field := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteNull()
			return
		}
		if t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}
		value, ok := db.hashKeys[key][field]
		if !ok {
			c.WriteNull()
			return
		}
		c.WriteBulk(value)
	})
}

// HDEL
func (m *Miniredis) cmdHdel(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(2)) {
		return
	}

	opts := struct {
		key    string
		fields []string
	}{
		key:    args[0],
		fields: args[1:],
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[opts.key]
		if !ok {
			// No key is zero deleted
			c.WriteInt(0)
			return
		}
		if t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		deleted := 0
		for _, f := range opts.fields {
			_, ok := db.hashKeys[opts.key][f]
			if !ok {
				continue
			}
			delete(db.hashKeys[opts.key], f)
			deleted++
		}
		c.WriteInt(deleted)

		// Nothing left. Remove the whole key.
		if len(db.hashKeys[opts.key]) == 0 {
			db.del(opts.key, true)
		}
	})
}

// HEXISTS
func (m *Miniredis) cmdHexists(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(2)) {
		return
	}

	opts := struct {
		key   string
		field string
	}{
		key:   args[0],
		field: args[1],
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[opts.key]
		if !ok {
			c.WriteInt(0)
			return
		}
		if t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		if _, ok := db.hashKeys[opts.key][opts.field]; !ok {
			c.WriteInt(0)
			return
		}
		c.WriteInt(1)
	})
}

// HGETALL
func (m *Miniredis) cmdHgetall(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteMapLen(0)
			return
		}
		if t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteMapLen(len(db.hashKeys[key]))
		for _, k := range db.hashFields(key) {
			c.WriteBulk(k)
			c.WriteBulk(db.hashGet(key, k))
		}
	})
}

// HKEYS
func (m *Miniredis) cmdHkeys(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteLen(0)
			return
		}
		if db.t(key) != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		fields := db.hashFields(key)
		c.WriteLen(len(fields))
		for _, f := range fields {
			c.WriteBulk(f)
		}
	})
}

// HSTRLEN
func (m *Miniredis) cmdHstrlen(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(2)) {
		return
	}

	hash, key := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[hash]
		if !ok {
			c.WriteInt(0)
			return
		}
		if t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		keys := db.hashKeys[hash]
		c.WriteInt(len(keys[key]))
	})
}

// HVALS
func (m *Miniredis) cmdHvals(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteLen(0)
			return
		}
		if t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		vals := db.hashValues(key)
		c.WriteLen(len(vals))
		for _, v := range vals {
			c.WriteBulk(v)
		}
	})
}

// HLEN
func (m *Miniredis) cmdHlen(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteInt(0)
			return
		}
		if t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteInt(len(db.hashKeys[key]))
	})
}

// HMGET
func (m *Miniredis) cmdHmget(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(2)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		f, ok := db.hashKeys[key]
		if !ok {
			f = map[string]string{}
		}

		c.WriteLen(len(args) - 1)
		for _, k := range args[1:] {
			v, ok := f[k]
			if !ok {
				c.WriteNull()
				continue
			}
			c.WriteBulk(v)
		}
	})
}

// HINCRBY
func (m *Miniredis) cmdHincrby(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(3)) {
		return
	}

	opts := struct {
		key   string
		field string
		delta int
	}{
		key:   args[0],
		field: args[1],
	}
	if ok := optInt(c, args[2], &opts.delta); !ok {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[opts.key]; ok && t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		v, err := db.hashIncr(opts.key, opts.field, opts.delta)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		c.WriteInt(v)
	})
}

// HINCRBYFLOAT
func (m *Miniredis) cmdHincrbyfloat(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(3)) {
		return
	}

	opts := struct {
		key   string
		field string
		delta *big.Float
	}{
		key:   args[0],
		field: args[1],
	}
	delta, _, err := big.ParseFloat(args[2], 10, 128, 0)
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidFloat)
		return
	}
	opts.delta = delta

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[opts.key]; ok && t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		v, err := db.hashIncrfloat(opts.key, opts.field, opts.delta)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		c.WriteBulk(formatBig(v))
	})
}

// HSCAN
func (m *Miniredis) cmdHscan(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(2)) {
		return
	}

	opts := struct {
		key       string
		cursor    int
		withMatch bool
		match     string
	}{
		key: args[0],
	}
	if ok := optIntErr(c, args[1], &opts.cursor, msgInvalidCursor); !ok {
		return
	}
	args = args[2:]

	// MATCH and COUNT options
	for len(args) > 0 {
		if strings.ToLower(args[0]) == "count" {
			// we do nothing with count
			if len(args) < 2 {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			_, err := strconv.Atoi(args[1])
			if err != nil {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			args = args[2:]
			continue
		}
		if strings.ToLower(args[0]) == "match" {
			if len(args) < 2 {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			opts.withMatch = true
			opts.match, args = args[1], args[2:]
			continue
		}
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)
		// return _all_ (matched) keys every time

		if opts.cursor != 0 {
			// Invalid cursor.
			c.WriteLen(2)
			c.WriteBulk("0") // no next cursor
			c.WriteLen(0)    // no elements
			return
		}
		if db.exists(opts.key) && db.t(opts.key) != keyTypeHash {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.hashFields(opts.key)
		if opts.withMatch {
			members, _ = matchKeys(members, opts.match)
		}

		c.WriteLen(2)
		c.WriteBulk("0") // no next cursor
		// HSCAN gives key, values.
		c.WriteLen(len(members) * 2)
		for _, k := range members {
			c.WriteBulk(k)
			c.WriteBulk(db.hashGet(opts.key, k))
		}
	})
}

// HRANDFIELD
func (m *Miniredis) cmdHrandfield(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, between(1, 3)) {
		return
	}

	opts := struct {
		key        string
		count      int
		countSet   bool
		withValues bool
	}{
		key: args[0],
	}

	if len(args) > 1 {
		if ok := optIntErr(c, args[1], &opts.count, msgInvalidInt); !ok {
			return
		}
		opts.countSet = true
	}

	if len(args) == 3 {
		if strings.ToLower(args[2]) == "withvalues" {
			opts.withValues = true
		} else {
			setDirty(c)
			c.WriteError(msgSyntaxError)
			return
		}
	}

	withTx(m, c, func(peer *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)
		members := db.hashFields(opts.key)
		m.shuffle(members)

		if !opts.countSet {
			// > When called with just the key argument, return a random field from the
			// hash value stored at key.
			if len(members) == 0 {
				peer.WriteNull()
				return
			}
			peer.WriteBulk(members[0])
			return
		}

		if len(members) > abs(opts.count) {
			members = members[:abs(opts.count)]
		}
		switch {
		case opts.count >= 0:
			// if count is positive there can't be duplicates, and the length is restricted
		case opts.count < 0:
			// if count is negative there can be duplicates, but length will match
			if len(members) > 0 {
				for len(members) < -opts.count {
					members = append(members, members[m.randIntn(len(members))])
				}
			}
		}

		if opts.withValues {
			peer.WriteMapLen(len(members))
			for _, m := range members {
				peer.WriteBulk(m)
				peer.WriteBulk(db.hashGet(opts.key, m))
			}
			return
		}
		peer.WriteLen(len(members))
		for _, m := range members {
			peer.WriteBulk(m)
		}
	})
}

// HEXPIRE
func (m *Miniredis) cmdHexpire(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(5)) {
		return
	}

	opts, err := parseHExpireArgs(args)
	if err != "" {
		setDirty(c)
		c.WriteError(err)
		return
	}

	withTx(m, c, func(peer *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if _, ok := db.keys[opts.key]; !ok {
			c.WriteLen(len(opts.fields))
			for range opts.fields {
				c.WriteInt(-2)
			}
			return
		}

		if db.t(opts.key) != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		fieldTTLs := db.hashTTLs[opts.key]
		if fieldTTLs == nil {
			fieldTTLs = map[string]time.Duration{}
			db.hashTTLs[opts.key] = fieldTTLs
		}

		c.WriteLen(len(opts.fields))
		for _, field := range opts.fields {
			if _, ok := db.hashKeys[opts.key][field]; !ok {
				c.WriteInt(-2)
				continue
			}

			currentTtl, ok := fieldTTLs[field]
			newTTL := time.Duration(opts.ttl) * time.Second

			// NX -- For each specified field,
			// set expiration only when the field has no expiration.
			if opts.nx && ok {
				c.WriteInt(0)
				continue
			}

			// XX -- For each specified field,
			// set expiration only when the field has an existing expiration.
			if opts.xx && !ok {
				c.WriteInt(0)
				continue
			}

			// GT -- For each specified field,
			// set expiration only when the new expiration is greater than current one.
			if opts.gt && (!ok || newTTL <= currentTtl) {
				c.WriteInt(0)
				continue
			}

			// LT -- For each specified field,
			// set expiration only when the new expiration is less than current one.
			if opts.lt && ok && newTTL >= currentTtl {
				c.WriteInt(0)
				continue
			}

			fieldTTLs[field] = newTTL
			c.WriteInt(1)
		}
	})
}

type hexpireOpts struct {
	key    string
	ttl    int
	nx     bool
	xx     bool
	gt     bool
	lt     bool
	fields []string
}

func parseHExpireArgs(args []string) (hexpireOpts, string) {
	var opts hexpireOpts
	opts.key = args[0]

	if err := optIntSimple(args[1], &opts.ttl); err != nil {
		return hexpireOpts{}, err.Error()
	}

	args = args[2:]

	for len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "nx":
			opts.nx = true
			args = args[1:]
		case "xx":
			opts.xx = true
			args = args[1:]
		case "gt":
			opts.gt = true
			args = args[1:]
		case "lt":
			opts.lt = true
			args = args[1:]
		case "fields":
			var numFields int
			if err := optIntSimple(args[1], &numFields); err != nil {
				return hexpireOpts{}, msgNumFieldsInvalid
			}
			if numFields <= 0 {
				return hexpireOpts{}, msgNumFieldsPositive
			}

			// FIELDS numFields field1 field2 ...
			if len(args) < 2+numFields {
				return hexpireOpts{}, msgNumFieldsParameter
			}

			opts.fields = append([]string{}, args[2:2+numFields]...)
			args = args[2+numFields:]
		default:
			return hexpireOpts{}, fmt.Sprintf(msgMandatoryArgument, "FIELDS")
		}
	}

	if opts.gt && opts.lt {
		return hexpireOpts{}, msgGTandLT
	}

	if opts.nx && (opts.xx || opts.gt || opts.lt) {
		return hexpireOpts{}, msgNXandXXGTLT
	}

	return opts, ""
}

// HPERSIST
func (m *Miniredis) cmdHpersist(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(3)) {
		return
	}

	key := args[0]
	fields, errMsg := parseFieldsArgs(args[1:])
	if errMsg != "" {
		setDirty(c)
		c.WriteError(errMsg)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if _, ok := db.keys[key]; !ok {
			c.WriteLen(len(fields))
			for range fields {
				c.WriteInt(-2)
			}
			return
		}

		if db.t(key) != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteLen(len(fields))
		for _, field := range fields {
			if _, ok := db.hashKeys[key][field]; !ok {
				c.WriteInt(-2)
				continue
			}

			fieldTTLs := db.hashTTLs[key]
			if fieldTTLs == nil {
				c.WriteInt(-1)
				continue
			}
			if _, ok := fieldTTLs[field]; !ok {
				c.WriteInt(-1)
				continue
			}

			delete(fieldTTLs, field)
			c.WriteInt(1)
		}
	})
}

// HTTL
func (m *Miniredis) cmdHttl(c *server.Peer, cmd string, args []string) {
	m.cmdHttlGeneric(c, cmd, args, time.Second)
}

// HPTTL
func (m *Miniredis) cmdHpttl(c *server.Peer, cmd string, args []string) {
	m.cmdHttlGeneric(c, cmd, args, time.Millisecond)
}

func (m *Miniredis) cmdHttlGeneric(c *server.Peer, cmd string, args []string, unit time.Duration) {
	if !m.isValidCMD(c, cmd, args, atLeast(3)) {
		return
	}

	key := args[0]
	fields, errMsg := parseFieldsArgs(args[1:])
	if errMsg != "" {
		setDirty(c)
		c.WriteError(errMsg)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if _, ok := db.keys[key]; !ok {
			c.WriteLen(len(fields))
			for range fields {
				c.WriteInt(-2)
			}
			return
		}

		if db.t(key) != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteLen(len(fields))
		for _, field := range fields {
			if _, ok := db.hashKeys[key][field]; !ok {
				c.WriteInt(-2)
				continue
			}

			fieldTTLs := db.hashTTLs[key]
			if fieldTTLs == nil {
				c.WriteInt(-1)
				continue
			}
			ttl, ok := fieldTTLs[field]
			if !ok {
				c.WriteInt(-1)
				continue
			}

			c.WriteInt(int(ttl / unit))
		}
	})
}

type hsetexOpts struct {
	key     string
	fnx     bool
	fxx     bool
	ttlMode string // "", "EX", "PX", "EXAT", "PXAT", "KEEPTTL"
	ttlVal  int    // raw value for EX/PX/EXAT/PXAT
	fields  []string
	values  []string
}

func parseHSetEXArgs(args []string) (hsetexOpts, string) {
	var opts hsetexOpts
	opts.key = args[0]
	args = args[1:]

	for len(args) > 0 {
		switch strings.ToUpper(args[0]) {
		case "FNX":
			opts.fnx = true
			args = args[1:]
		case "FXX":
			opts.fxx = true
			args = args[1:]
		case "KEEPTTL":
			if opts.ttlMode != "" {
				return hsetexOpts{}, msgSyntaxError
			}
			opts.ttlMode = "KEEPTTL"
			args = args[1:]
		case "EX", "PX", "EXAT", "PXAT":
			if opts.ttlMode != "" {
				return hsetexOpts{}, "ERR Only one of EX, PX, EXAT, PXAT or KEEPTTL arguments can be specified"
			}
			mode := strings.ToUpper(args[0])
			if len(args) < 2 {
				return hsetexOpts{}, msgInvalidInt
			}
			var val int
			if err := optIntSimple(args[1], &val); err != nil {
				return hsetexOpts{}, msgInvalidInt
			}
			if val <= 0 {
				return hsetexOpts{}, "ERR invalid expire time, must be >= 0"
			}
			opts.ttlMode = mode
			opts.ttlVal = val
			args = args[2:]
		case "FIELDS":
			if len(args) < 2 {
				return hsetexOpts{}, msgNumFieldsInvalid
			}
			var numFields int
			if err := optIntSimple(args[1], &numFields); err != nil {
				return hsetexOpts{}, msgNumFieldsInvalid
			}
			if numFields <= 0 {
				return hsetexOpts{}, msgNumFieldsPositive
			}
			// Need numFields * 2 args (field value pairs)
			if len(args) < 2+numFields*2 {
				return hsetexOpts{}, msgNumFieldsParameter
			}
			if len(args) > 2+numFields*2 {
				return hsetexOpts{}, msgNumFieldsParameter
			}
			fvArgs := args[2 : 2+numFields*2]
			for i := 0; i < len(fvArgs); i += 2 {
				opts.fields = append(opts.fields, fvArgs[i])
				opts.values = append(opts.values, fvArgs[i+1])
			}
			args = args[2+numFields*2:]
		default:
			return hsetexOpts{}, msgSyntaxError
		}
	}

	if opts.fnx && opts.fxx {
		return hsetexOpts{}, msgSyntaxError
	}

	if len(opts.fields) == 0 {
		return hsetexOpts{}, fmt.Sprintf(msgMandatoryArgument, "FIELDS")
	}

	return opts, ""
}

// HSETEX
func (m *Miniredis) cmdHsetex(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(4)) {
		return
	}

	opts, errMsg := parseHSetEXArgs(args)
	if errMsg != "" {
		setDirty(c)
		c.WriteError(errMsg)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[opts.key]; ok && t != keyTypeHash {
			c.WriteError(msgWrongType)
			return
		}

		// FNX: only set if none of the specified fields exist
		if opts.fnx {
			for _, field := range opts.fields {
				if _, ok := db.hashKeys[opts.key][field]; ok {
					c.WriteInt(0)
					return
				}
			}
		}

		// FXX: only set if all of the specified fields exist
		if opts.fxx {
			for _, field := range opts.fields {
				if _, ok := db.hashKeys[opts.key][field]; !ok {
					c.WriteInt(0)
					return
				}
			}
		}

		// Resolve TTL
		var ttl time.Duration
		hasTTL := false
		keepTTL := false
		switch opts.ttlMode {
		case "EX":
			ttl = time.Duration(opts.ttlVal) * time.Second
			hasTTL = true
		case "PX":
			ttl = time.Duration(opts.ttlVal) * time.Millisecond
			hasTTL = true
		case "EXAT":
			ttl = m.at(opts.ttlVal, time.Second)
			hasTTL = true
		case "PXAT":
			ttl = m.at(opts.ttlVal, time.Millisecond)
			hasTTL = true
		case "KEEPTTL":
			keepTTL = true
		}

		// Set all fields
		for i, field := range opts.fields {
			db.hashSet(opts.key, field, opts.values[i])

			if keepTTL {
				// Don't touch existing TTL
				continue
			}

			// Initialize TTL map if needed
			if db.hashTTLs[opts.key] == nil {
				if hasTTL {
					db.hashTTLs[opts.key] = map[string]time.Duration{}
				}
			}

			if hasTTL {
				db.hashTTLs[opts.key][field] = ttl
			} else {
				// No TTL option: remove any existing TTL
				if fieldTTLs := db.hashTTLs[opts.key]; fieldTTLs != nil {
					delete(fieldTTLs, field)
				}
			}
		}

		c.WriteInt(1)
	})
}

// parseFieldsArgs parses "FIELDS numfields field [field ...]" from args.
// Returns the parsed field names, or an error string.
func parseFieldsArgs(args []string) ([]string, string) {
	if len(args) < 2 {
		return nil, fmt.Sprintf(msgMandatoryArgument, "FIELDS")
	}

	if strings.ToLower(args[0]) != "fields" {
		return nil, fmt.Sprintf(msgMandatoryArgument, "FIELDS")
	}

	var numFields int
	if err := optIntSimple(args[1], &numFields); err != nil {
		return nil, msgNumFieldsInvalid
	}
	if numFields <= 0 {
		return nil, msgNumFieldsPositive
	}

	if len(args) < 2+numFields {
		return nil, msgNumFieldsParameter
	}

	// Reject trailing args after the declared fields
	if len(args) > 2+numFields {
		return nil, msgNumFieldsParameter
	}

	return append([]string{}, args[2:2+numFields]...), ""
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
