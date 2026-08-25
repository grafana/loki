// Commands from https://redis.io/commands#generic

package miniredis

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicebob/miniredis/v2/server"
)

const (
	// expiretimeReplyNoExpiration is return value for EXPIRETIME and PEXPIRETIME if the key exists but has no associated expiration time
	expiretimeReplyNoExpiration = -1
	// expiretimeReplyMissingKey is return value for EXPIRETIME and PEXPIRETIME if the key does not exist
	expiretimeReplyMissingKey = -2
)

func inSeconds(t time.Time) int {
	return int(t.Unix())
}

func inMilliSeconds(t time.Time) int {
	return int(t.UnixMilli())
}

// commandsGeneric handles EXPIRE, TTL, PERSIST, &c.
func commandsGeneric(m *Miniredis) {
	m.srv.Register("COPY", m.cmdCopy)
	m.srv.Register("DEL", m.cmdDel)
	m.srv.Register("DUMP", m.cmdDump, server.ReadOnlyOption())
	m.srv.Register("EXISTS", m.cmdExists, server.ReadOnlyOption())
	m.srv.Register("EXPIRE", makeCmdExpire(m, false, time.Second))
	m.srv.Register("EXPIREAT", makeCmdExpire(m, true, time.Second))
	m.srv.Register("EXPIRETIME", m.makeCmdExpireTime(inSeconds), server.ReadOnlyOption())
	m.srv.Register("PEXPIRETIME", m.makeCmdExpireTime(inMilliSeconds), server.ReadOnlyOption())
	m.srv.Register("KEYS", m.cmdKeys, server.ReadOnlyOption())
	// MIGRATE
	m.srv.Register("MOVE", m.cmdMove)
	// OBJECT
	m.srv.Register("PERSIST", m.cmdPersist)
	m.srv.Register("PEXPIRE", makeCmdExpire(m, false, time.Millisecond))
	m.srv.Register("PEXPIREAT", makeCmdExpire(m, true, time.Millisecond))
	m.srv.Register("PTTL", m.cmdPTTL, server.ReadOnlyOption())
	m.srv.Register("RANDOMKEY", m.cmdRandomkey, server.ReadOnlyOption())
	m.srv.Register("RENAME", m.cmdRename)
	m.srv.Register("RENAMENX", m.cmdRenamenx)
	m.srv.Register("RESTORE", m.cmdRestore)
	m.srv.Register("TOUCH", m.cmdTouch, server.ReadOnlyOption())
	m.srv.Register("TTL", m.cmdTTL, server.ReadOnlyOption())
	m.srv.Register("TYPE", m.cmdType, server.ReadOnlyOption())
	m.srv.Register("SCAN", m.cmdScan, server.ReadOnlyOption())
	// SORT
	m.srv.Register("UNLINK", m.cmdDel)
	m.srv.Register("WAIT", m.cmdWait)
}

type expireOpts struct {
	key   string
	value int
	nx    bool
	xx    bool
	gt    bool
	lt    bool
}

func expireParse(cmd string, args []string) (*expireOpts, error) {
	var opts expireOpts

	opts.key = args[0]
	if err := optIntSimple(args[1], &opts.value); err != nil {
		return nil, err
	}
	args = args[2:]
	for len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "nx":
			opts.nx = true
		case "xx":
			opts.xx = true
		case "gt":
			opts.gt = true
		case "lt":
			opts.lt = true
		default:
			return nil, fmt.Errorf("ERR Unsupported option %s", args[0])
		}
		args = args[1:]
	}
	if opts.gt && opts.lt {
		return nil, errors.New("ERR GT and LT options at the same time are not compatible")
	}
	if opts.nx && (opts.xx || opts.gt || opts.lt) {
		return nil, errors.New("ERR NX and XX, GT or LT options at the same time are not compatible")
	}
	return &opts, nil
}

// generic expire command for EXPIRE, PEXPIRE, EXPIREAT, PEXPIREAT
// d is the time unit. If unix is set it'll be seen as a unixtimestamp and
// converted to a duration.
func makeCmdExpire(m *Miniredis, unix bool, d time.Duration) func(*server.Peer, string, []string) {
	return func(c *server.Peer, cmd string, args []string) {
		if !m.isValidCMD(c, cmd, args, atLeast(2)) {
			return
		}

		opts, err := expireParse(cmd, args)
		if err != nil {
			setDirty(c)
			c.WriteError(err.Error())
			return
		}

		withTx(m, c, func(c *server.Peer, ctx *connCtx) {
			db := m.db(ctx.selectedDB)

			// Key must be present.
			if _, ok := db.keys[opts.key]; !ok {
				c.WriteInt(0)
				return
			}

			oldTTL, ok := db.ttl[opts.key]

			var newTTL time.Duration
			if unix {
				newTTL = m.at(opts.value, d)
			} else {
				newTTL = time.Duration(opts.value) * d
			}

			// > NX -- Set expiry only when the key has no expiry
			if opts.nx && ok {
				c.WriteInt(0)
				return
			}
			// > XX -- Set expiry only when the key has an existing expiry
			if opts.xx && !ok {
				c.WriteInt(0)
				return
			}
			// > GT -- Set expiry only when the new expiry is greater than current one
			// (no exp == infinity)
			if opts.gt && (!ok || newTTL <= oldTTL) {
				c.WriteInt(0)
				return
			}
			// > LT -- Set expiry only when the new expiry is less than current one
			if opts.lt && ok && newTTL > oldTTL {
				c.WriteInt(0)
				return
			}
			db.ttl[opts.key] = newTTL
			db.incr(opts.key)
			db.checkTTL(opts.key)
			c.WriteInt(1)
		})
	}
}

// makeCmdExpireTime creates server command function that returns the absolute Unix timestamp (since January 1, 1970)
// at which the given key will expire, in unit selected by time result strategy (e.g. seconds, milliseconds).
// For more information see redis documentation for [expiretime] and [pexpiretime].
//
// [expiretime]: https://redis.io/commands/expiretime/
// [pexpiretime]: https://redis.io/commands/pexpiretime/
func (m *Miniredis) makeCmdExpireTime(timeResultStrategy func(time.Time) int) server.Cmd {
	return func(c *server.Peer, cmd string, args []string) {
		if !m.isValidCMD(c, cmd, args, exactly(1)) {
			return
		}

		key := args[0]
		withTx(m, c, func(c *server.Peer, ctx *connCtx) {
			db := m.db(ctx.selectedDB)

			if _, ok := db.keys[key]; !ok {
				c.WriteInt(expiretimeReplyMissingKey)
				return
			}

			ttl, ok := db.ttl[key]
			if !ok {
				c.WriteInt(expiretimeReplyNoExpiration)
				return
			}

			c.WriteInt(timeResultStrategy(m.effectiveNow().Add(ttl)))
		})
	}
}

// TOUCH
func (m *Miniredis) cmdTouch(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(1)) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		count := 0
		for _, key := range args {
			if db.exists(key) {
				count++
			}
		}
		c.WriteInt(count)
	})
}

// TTL
func (m *Miniredis) cmdTTL(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if _, ok := db.keys[key]; !ok {
			// No such key
			c.WriteInt(-2)
			return
		}

		v, ok := db.ttl[key]
		if !ok {
			// no expire value
			c.WriteInt(-1)
			return
		}
		c.WriteInt(int(v.Seconds()))
	})
}

// PTTL
func (m *Miniredis) cmdPTTL(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if _, ok := db.keys[key]; !ok {
			// no such key
			c.WriteInt(-2)
			return
		}

		v, ok := db.ttl[key]
		if !ok {
			// no expire value
			c.WriteInt(-1)
			return
		}
		c.WriteInt(int(v.Nanoseconds() / 1000000))
	})
}

// PERSIST
func (m *Miniredis) cmdPersist(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if _, ok := db.keys[key]; !ok {
			// no such key
			c.WriteInt(0)
			return
		}

		if _, ok := db.ttl[key]; !ok {
			// no expire value
			c.WriteInt(0)
			return
		}
		delete(db.ttl, key)
		db.incr(key)
		c.WriteInt(1)
	})
}

// DEL and UNLINK
func (m *Miniredis) cmdDel(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(1)) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		count := 0
		for _, key := range args {
			if db.exists(key) {
				count++
			}
			db.del(key, true) // delete expire
		}
		c.WriteInt(count)
	})
}

// DUMP
func (m *Miniredis) cmdDump(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)
		keyType, exists := db.keys[key]
		if !exists {
			c.WriteNull()
		} else if keyType != keyTypeString {
			c.WriteError(msgWrongType)
		} else {
			c.WriteBulk(db.stringGet(key))
		}
	})
}

// TYPE
func (m *Miniredis) cmdType(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteInline("none")
			return
		}

		c.WriteInline(t)
	})
}

// EXISTS
func (m *Miniredis) cmdExists(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(1)) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		found := 0
		for _, k := range args {
			if db.exists(k) {
				found++
			}
		}
		c.WriteInt(found)
	})
}

// MOVE
func (m *Miniredis) cmdMove(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(2)) {
		return
	}

	var opts struct {
		key      string
		targetDB int
	}

	opts.key = args[0]
	opts.targetDB, _ = strconv.Atoi(args[1])

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		if ctx.selectedDB == opts.targetDB {
			c.WriteError("ERR source and destination objects are the same")
			return
		}
		db := m.db(ctx.selectedDB)
		targetDB := m.db(opts.targetDB)

		if !db.move(opts.key, targetDB) {
			c.WriteInt(0)
			return
		}
		c.WriteInt(1)
	})
}

// KEYS
func (m *Miniredis) cmdKeys(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(1)) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		keys, _ := matchKeys(db.allKeys(), key)
		c.WriteLen(len(keys))
		for _, s := range keys {
			c.WriteBulk(s)
		}
	})
}

// RANDOMKEY
func (m *Miniredis) cmdRandomkey(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(0)) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if len(db.keys) == 0 {
			c.WriteNull()
			return
		}
		nr := m.randIntn(len(db.keys))
		for k := range db.keys {
			if nr == 0 {
				c.WriteBulk(k)
				return
			}
			nr--
		}
	})
}

// RENAME
func (m *Miniredis) cmdRename(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(2)) {
		return
	}

	opts := struct {
		from string
		to   string
	}{
		from: args[0],
		to:   args[1],
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(opts.from) {
			c.WriteError(msgKeyNotFound)
			return
		}

		db.rename(opts.from, opts.to)
		c.WriteOK()
	})
}

// RENAMENX
func (m *Miniredis) cmdRenamenx(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(2)) {
		return
	}

	opts := struct {
		from string
		to   string
	}{
		from: args[0],
		to:   args[1],
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(opts.from) {
			c.WriteError(msgKeyNotFound)
			return
		}

		if db.exists(opts.to) {
			c.WriteInt(0)
			return
		}

		db.rename(opts.from, opts.to)
		c.WriteInt(1)
	})
}

type restoreOpts struct {
	key             string
	serializedValue string
	rawTtl          string
	replace         bool
	absTtl          bool
}

func restoreParse(args []string) *restoreOpts {
	var opts restoreOpts

	opts.key, opts.rawTtl, opts.serializedValue, args = args[0], args[1], args[2], args[3:]

	for len(args) > 0 {
		switch arg := strings.ToUpper(args[0]); arg {
		case "REPLACE":
			opts.replace = true
		case "ABSTTL":
			opts.absTtl = true
		default:
			return nil
		}

		args = args[1:]
	}

	return &opts
}

// RESTORE
func (m *Miniredis) cmdRestore(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(3)) {
		return
	}

	var opts = restoreParse(args)
	if opts == nil {
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		_, keyExists := db.keys[opts.key]
		if keyExists && !opts.replace {
			setDirty(c)
			c.WriteError("BUSYKEY Target key name already exists.")
			return
		}

		ttl, err := strconv.Atoi(opts.rawTtl)
		if err != nil || ttl < 0 {
			c.WriteError(msgInvalidInt)
			return
		}

		db.stringSet(opts.key, opts.serializedValue)

		if ttl != 0 {
			if opts.absTtl {
				db.ttl[opts.key] = m.at(ttl, time.Millisecond)
			} else {
				db.ttl[opts.key] = time.Duration(ttl) * time.Millisecond
			}
		}

		c.WriteOK()
	})
}

type scanOpts struct {
	cursor    int
	count     int
	withMatch bool
	match     string
	withType  bool
	_type     string
}

func scanParse(cmd string, args []string) (*scanOpts, error) {
	var opts scanOpts
	if err := optIntSimple(args[0], &opts.cursor); err != nil {
		return nil, errors.New(msgInvalidCursor)
	}
	args = args[1:]

	// MATCH, COUNT and TYPE options
	for len(args) > 0 {
		if strings.ToLower(args[0]) == "count" {
			if len(args) < 2 {
				return nil, errors.New(msgSyntaxError)
			}
			count, err := strconv.Atoi(args[1])
			if err != nil || count < 0 {
				return nil, errors.New(msgInvalidInt)
			}
			if count == 0 {
				return nil, errors.New(msgSyntaxError)
			}
			opts.count = count
			args = args[2:]
			continue
		}
		if strings.ToLower(args[0]) == "match" {
			if len(args) < 2 {
				return nil, errors.New(msgSyntaxError)
			}
			opts.withMatch = true
			opts.match, args = args[1], args[2:]
			continue
		}
		if strings.ToLower(args[0]) == "type" {
			if len(args) < 2 {
				return nil, errors.New(msgSyntaxError)
			}
			opts.withType = true
			opts._type, args = strings.ToLower(args[1]), args[2:]
			continue
		}
		return nil, errors.New(msgSyntaxError)
	}
	return &opts, nil
}

// SCAN
func (m *Miniredis) cmdScan(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(1)) {
		return
	}

	opts, err := scanParse(cmd, args)
	if err != nil {
		setDirty(c)
		c.WriteError(err.Error())
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)
		// We return _all_ (matched) keys every time, so that cursors work.
		// We ignore "COUNT", which is allowed according to the Redis docs.
		var keys []string

		if opts.withType {
			keys = make([]string, 0)
			for k, t := range db.keys {
				// type must be given exactly; no pattern matching is performed
				if t == opts._type {
					keys = append(keys, k)
				}
			}
		} else {
			keys = db.allKeys()
		}

		sort.Strings(keys) // To make things deterministic.

		if opts.withMatch {
			keys, _ = matchKeys(keys, opts.match)
		}

		// we only ever return all at once, so no non-zero cursor can every be valid
		if opts.cursor != 0 {
			c.WriteLen(2)
			c.WriteBulk("0") // no next cursor
			c.WriteLen(0)    // no elements
			return
		}
		cursorValue := 0 // we don't use cursors
		c.WriteLen(2)
		c.WriteBulk(fmt.Sprintf("%d", cursorValue))
		c.WriteLen(len(keys))
		for _, k := range keys {
			c.WriteBulk(k)
		}
	})
}

type copyOpts struct {
	from          string
	to            string
	destinationDB int
	replace       bool
}

func copyParse(cmd string, args []string) (*copyOpts, error) {
	opts := copyOpts{
		destinationDB: -1,
	}

	opts.from, opts.to, args = args[0], args[1], args[2:]
	for len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "db":
			if len(args) < 2 {
				return nil, errors.New(msgSyntaxError)
			}
			if err := optIntSimple(args[1], &opts.destinationDB); err != nil {
				return nil, err
			}
			if opts.destinationDB < 0 {
				return nil, errors.New(msgDBIndexOutOfRange)
			}
			args = args[2:]
		case "replace":
			opts.replace = true
			args = args[1:]
		default:
			return nil, errors.New(msgSyntaxError)
		}
	}
	return &opts, nil
}

// COPY
func (m *Miniredis) cmdCopy(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(2)) {
		return
	}

	opts, err := copyParse(cmd, args)
	if err != nil {
		setDirty(c)
		c.WriteError(err.Error())
		return
	}
	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		fromDB, toDB := ctx.selectedDB, opts.destinationDB
		if toDB == -1 {
			toDB = fromDB
		}

		if fromDB == toDB && opts.from == opts.to {
			c.WriteError("ERR source and destination objects are the same")
			return
		}

		if !m.db(fromDB).exists(opts.from) {
			c.WriteInt(0)
			return
		}

		if !opts.replace {
			if m.db(toDB).exists(opts.to) {
				c.WriteInt(0)
				return
			}
		}

		m.copy(m.db(fromDB), opts.from, m.db(toDB), opts.to)
		c.WriteInt(1)
	})
}

// WAIT
func (m *Miniredis) cmdWait(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, exactly(2)) {
		return
	}
	nReplicas, err := strconv.Atoi(args[0])
	if err != nil || nReplicas < 0 {
		c.WriteError(msgInvalidInt)
		return
	}
	timeout, err := strconv.Atoi(args[1])
	if err != nil {
		c.WriteError(msgInvalidInt)
		return
	}
	if timeout < 0 {
		c.WriteError(msgTimeoutNegative)
		return
	}
	// WAIT always returns 0 when called on a standalone instance
	c.WriteInt(0)
}
