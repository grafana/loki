// Commands from https://redis.io/commands#geo

package miniredis

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/alicebob/miniredis/v2/server"
)

// commandsGeo handles GEOADD, GEORADIUS etc.
func commandsGeo(m *Miniredis) {
	m.srv.Register("GEOADD", m.cmdGeoadd)
	m.srv.Register("GEODIST", m.cmdGeodist)
	m.srv.Register("GEOPOS", m.cmdGeopos)
	m.srv.Register("GEORADIUS", m.cmdGeoradius)
	m.srv.Register("GEORADIUS_RO", m.cmdGeoradius)
	m.srv.Register("GEORADIUSBYMEMBER", m.cmdGeoradiusbymember)
	m.srv.Register("GEORADIUSBYMEMBER_RO", m.cmdGeoradiusbymember)
}

// GEOADD
func (m *Miniredis) cmdGeoadd(c *server.Peer, cmd string, args []string) {
	if len(args) < 3 || len(args[1:])%3 != 0 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}
	if m.checkPubsub(c, cmd) {
		return
	}
	key, args := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if db.exists(key) && db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		toSet := map[string]float64{}
		for len(args) > 2 {
			rawLong, rawLat, name := args[0], args[1], args[2]
			args = args[3:]
			longitude, err := strconv.ParseFloat(rawLong, 64)
			if err != nil {
				c.WriteError("ERR value is not a valid float")
				return
			}
			latitude, err := strconv.ParseFloat(rawLat, 64)
			if err != nil {
				c.WriteError("ERR value is not a valid float")
				return
			}

			if latitude < -85.05112878 ||
				latitude > 85.05112878 ||
				longitude < -180 ||
				longitude > 180 {
				c.WriteError(fmt.Sprintf("ERR invalid longitude,latitude pair %.6f,%.6f", longitude, latitude))
				return
			}

			toSet[name] = float64(toGeohash(longitude, latitude))
		}

		set := 0
		for name, score := range toSet {
			if db.ssetAdd(key, score, name) {
				set++
			}
		}
		c.WriteInt(set)
	})
}

// GEODIST
func (m *Miniredis) cmdGeodist(c *server.Peer, cmd string, args []string) {
	if len(args) < 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}
	if m.checkPubsub(c, cmd) {
		return
	}

	key, from, to, args := args[0], args[1], args[2], args[3:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)
		if !db.exists(key) {
			c.WriteNull()
			return
		}
		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		unit := "m"
		if len(args) > 0 {
			unit, args = args[0], args[1:]
		}
		if len(args) > 0 {
			c.WriteError(msgSyntaxError)
			return
		}

		toMeter := parseUnit(unit)
		if toMeter == 0 {
			c.WriteError(msgUnsupportedUnit)
			return
		}

		members := db.sortedsetKeys[key]
		fromD, okFrom := members.get(from)
		toD, okTo := members.get(to)
		if !okFrom || !okTo {
			c.WriteNull()
			return
		}

		fromLo, fromLat := fromGeohash(uint64(fromD))
		toLo, toLat := fromGeohash(uint64(toD))

		dist := distance(fromLat, fromLo, toLat, toLo) / toMeter
		c.WriteBulk(fmt.Sprintf("%.4f", dist))
	})
}

// GEOPOS
func (m *Miniredis) cmdGeopos(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}
	if m.checkPubsub(c, cmd) {
		return
	}
	key, args := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if db.exists(key) && db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		c.WriteLen(len(args))
		for _, l := range args {
			if !db.ssetExists(key, l) {
				c.WriteLen(-1)
				continue
			}
			score := db.ssetScore(key, l)
			c.WriteLen(2)
			long, lat := fromGeohash(uint64(score))
			c.WriteBulk(fmt.Sprintf("%f", long))
			c.WriteBulk(fmt.Sprintf("%f", lat))
		}
	})
}

type geoDistance struct {
	Name      string
	Score     float64
	Distance  float64
	Longitude float64
	Latitude  float64
}

// GEORADIUS and GEORADIUS_RO
func (m *Miniredis) cmdGeoradius(c *server.Peer, cmd string, args []string) {
	if len(args) < 5 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}
	if m.checkPubsub(c, cmd) {
		return
	}

	key := args[0]
	longitude, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	latitude, err := strconv.ParseFloat(args[2], 64)
	if err != nil {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	radius, err := strconv.ParseFloat(args[3], 64)
	if err != nil || radius < 0 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	toMeter := parseUnit(args[4])
	if toMeter == 0 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	args = args[5:]

	var (
		withDist      = false
		withCoord     = false
		direction     = unsorted
		count         = 0
		withStore     = false
		storeKey      = ""
		withStoredist = false
		storedistKey  = ""
	)
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		switch strings.ToUpper(arg) {
		case "WITHCOORD":
			withCoord = true
		case "WITHDIST":
			withDist = true
		case "ASC":
			direction = asc
		case "DESC":
			direction = desc
		case "COUNT":
			if len(args) == 0 {
				setDirty(c)
				c.WriteError("ERR syntax error")
				return
			}
			n, err := strconv.Atoi(args[0])
			if err != nil {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			if n <= 0 {
				setDirty(c)
				c.WriteError("ERR COUNT must be > 0")
				return
			}
			args = args[1:]
			count = n
		case "STORE":
			if len(args) == 0 {
				setDirty(c)
				c.WriteError("ERR syntax error")
				return
			}
			withStore = true
			storeKey = args[0]
			args = args[1:]
		case "STOREDIST":
			if len(args) == 0 {
				setDirty(c)
				c.WriteError("ERR syntax error")
				return
			}
			withStoredist = true
			storedistKey = args[0]
			args = args[1:]
		default:
			setDirty(c)
			c.WriteError("ERR syntax error")
			return
		}
	}

	if strings.ToUpper(cmd) == "GEORADIUS_RO" && (withStore || withStoredist) {
		setDirty(c)
		c.WriteError("ERR syntax error")
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		if (withStore || withStoredist) && (withDist || withCoord) {
			c.WriteError("ERR STORE option in GEORADIUS is not compatible with WITHDIST, WITHHASH and WITHCOORDS options")
			return
		}

		db := m.db(ctx.selectedDB)
		members := db.ssetElements(key)

		matches := withinRadius(members, longitude, latitude, radius*toMeter)

		// deal with ASC/DESC
		if direction != unsorted {
			sort.Slice(matches, func(i, j int) bool {
				if direction == desc {
					return matches[i].Distance > matches[j].Distance
				}
				return matches[i].Distance < matches[j].Distance
			})
		}

		// deal with COUNT
		if count > 0 && len(matches) > count {
			matches = matches[:count]
		}

		// deal with "STORE x"
		if withStore {
			db.del(storeKey, true)
			for _, member := range matches {
				db.ssetAdd(storeKey, member.Score, member.Name)
			}
			c.WriteInt(len(matches))
			return
		}

		// deal with "STOREDIST x"
		if withStoredist {
			db.del(storedistKey, true)
			for _, member := range matches {
				db.ssetAdd(storedistKey, member.Distance/toMeter, member.Name)
			}
			c.WriteInt(len(matches))
			return
		}

		c.WriteLen(len(matches))
		for _, member := range matches {
			if !withDist && !withCoord {
				c.WriteBulk(member.Name)
				continue
			}

			len := 1
			if withDist {
				len++
			}
			if withCoord {
				len++
			}
			c.WriteLen(len)
			c.WriteBulk(member.Name)
			if withDist {
				c.WriteBulk(fmt.Sprintf("%.4f", member.Distance/toMeter))
			}
			if withCoord {
				c.WriteLen(2)
				c.WriteBulk(fmt.Sprintf("%f", member.Longitude))
				c.WriteBulk(fmt.Sprintf("%f", member.Latitude))
			}
		}
	})
}

// GEORADIUSBYMEMBER and GEORADIUSBYMEMBER_RO
func (m *Miniredis) cmdGeoradiusbymember(c *server.Peer, cmd string, args []string) {
	if len(args) < 4 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}
	if m.checkPubsub(c, cmd) {
		return
	}

	key := args[0]
	member := args[1]

	radius, err := strconv.ParseFloat(args[2], 64)
	if err != nil || radius < 0 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	toMeter := parseUnit(args[3])
	if toMeter == 0 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	args = args[4:]

	var (
		withDist      = false
		withCoord     = false
		direction     = unsorted
		count         = 0
		withStore     = false
		storeKey      = ""
		withStoredist = false
		storedistKey  = ""
	)
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		switch strings.ToUpper(arg) {
		case "WITHCOORD":
			withCoord = true
		case "WITHDIST":
			withDist = true
		case "ASC":
			direction = asc
		case "DESC":
			direction = desc
		case "COUNT":
			if len(args) == 0 {
				setDirty(c)
				c.WriteError("ERR syntax error")
				return
			}
			n, err := strconv.Atoi(args[0])
			if err != nil {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			if n <= 0 {
				setDirty(c)
				c.WriteError("ERR COUNT must be > 0")
				return
			}
			args = args[1:]
			count = n
		case "STORE":
			if len(args) == 0 {
				setDirty(c)
				c.WriteError("ERR syntax error")
				return
			}
			withStore = true
			storeKey = args[0]
			args = args[1:]
		case "STOREDIST":
			if len(args) == 0 {
				setDirty(c)
				c.WriteError("ERR syntax error")
				return
			}
			withStoredist = true
			storedistKey = args[0]
			args = args[1:]
		default:
			setDirty(c)
			c.WriteError("ERR syntax error")
			return
		}
	}

	if strings.ToUpper(cmd) == "GEORADIUSBYMEMBER_RO" && (withStore || withStoredist) {
		setDirty(c)
		c.WriteError("ERR syntax error")
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		if (withStore || withStoredist) && (withDist || withCoord) {
			c.WriteError("ERR STORE option in GEORADIUS is not compatible with WITHDIST, WITHHASH and WITHCOORDS options")
			return
		}

		db := m.db(ctx.selectedDB)
		if !db.exists(key) {
			c.WriteNull()
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		// get position of member
		if !db.ssetExists(key, member) {
			c.WriteError("ERR could not decode requested zset member")
			return
		}
		score := db.ssetScore(key, member)
		longitude, latitude := fromGeohash(uint64(score))

		members := db.ssetElements(key)
		matches := withinRadius(members, longitude, latitude, radius*toMeter)

		// deal with ASC/DESC
		if direction != unsorted {
			sort.Slice(matches, func(i, j int) bool {
				if direction == desc {
					return matches[i].Distance > matches[j].Distance
				}
				return matches[i].Distance < matches[j].Distance
			})
		}

		// deal with COUNT
		if count > 0 && len(matches) > count {
			matches = matches[:count]
		}

		// deal with "STORE x"
		if withStore {
			db.del(storeKey, true)
			for _, member := range matches {
				db.ssetAdd(storeKey, member.Score, member.Name)
			}
			c.WriteInt(len(matches))
			return
		}

		// deal with "STOREDIST x"
		if withStoredist {
			db.del(storedistKey, true)
			for _, member := range matches {
				db.ssetAdd(storedistKey, member.Distance/toMeter, member.Name)
			}
			c.WriteInt(len(matches))
			return
		}

		c.WriteLen(len(matches))
		for _, member := range matches {
			if !withDist && !withCoord {
				c.WriteBulk(member.Name)
				continue
			}

			len := 1
			if withDist {
				len++
			}
			if withCoord {
				len++
			}
			c.WriteLen(len)
			c.WriteBulk(member.Name)
			if withDist {
				c.WriteBulk(fmt.Sprintf("%.4f", member.Distance/toMeter))
			}
			if withCoord {
				c.WriteLen(2)
				c.WriteBulk(fmt.Sprintf("%f", member.Longitude))
				c.WriteBulk(fmt.Sprintf("%f", member.Latitude))
			}
		}
	})
}

func withinRadius(members []ssElem, longitude, latitude, radius float64) []geoDistance {
	matches := []geoDistance{}
	for _, el := range members {
		elLo, elLat := fromGeohash(uint64(el.score))
		distanceInMeter := distance(latitude, longitude, elLat, elLo)

		if distanceInMeter <= radius {
			matches = append(matches, geoDistance{
				Name:      el.member,
				Score:     el.score,
				Distance:  distanceInMeter,
				Longitude: elLo,
				Latitude:  elLat,
			})
		}
	}
	return matches
}

func parseUnit(u string) float64 {
	switch u {
	case "m":
		return 1
	case "km":
		return 1000
	case "mi":
		return 1609.34
	case "ft":
		return 0.3048
	default:
		return 0
	}
}
