## Changelog


### v2.30.4

- fix ZADD LT/LG (thanks @sejin-P)
- fix COPY (thanks @jerargus)
- quicker SPOP


### v2.30.3

- fix lua error_reply (thanks @pkierski)
- fix use of blocking functions in lua
- support for ZMSCORE (thanks @lsgndln)
- lua cache (thanks @tonyhb)


### v2.30.2

- support MINID in XADD  (thanks @nathan-cormier)
- support BLMOVE (thanks @sevein)
- fix COMMAND (thanks @pje)
- fix 'XREAD ... $' on a non-existing stream


### v2.30.1

- support SET NX GET special case


### v2.30.0

- implement redis 7.0.x (from 6.X). Main changes:
   - test against 7.0.7
   - update error messages
   - support nx|xx|gt|lt options in [P]EXPIRE[AT]
   - update how deleted items are processed in pending queues in streams


### v2.23.1

- resolve $ to latest ID in XREAD (thanks @josh-hook)
- handle disconnect in blocking functions (thanks @jgirtakovskis)
- fix type conversion bug in redisToLua (thanks Sandy Harvie)
- BRPOP{LPUSH} timeout can be float since 6.0


### v2.23.0

- basic INFO support (thanks @kirill-a-belov)
- support COUNT in SSCAN (thanks @Abdi-dd)
- test and support Go 1.19
- support LPOS (thanks @ianstarz)
- support XPENDING, XGROUP {CREATECONSUMER,DESTROY,DELCONSUMER}, XINFO {CONSUMERS,GROUPS}, XCLAIM (thanks @sandyharvie)


### v2.22.0

- set miniredis.DumpMaxLineLen to get more Dump() info (thanks @afjoseph)
- fix invalid resposne of COMMAND (thanks @zsh1995)
- fix possibility to generate duplicate IDs in XADD (thanks @readams)
- adds support for XAUTOCLAIM min-idle parameter (thanks @readams)


### v2.21.0

- support for GETEX (thanks @dntj)
- support for GT and LT in ZADD (thanks @lsgndln)
- support for XAUTOCLAIM (thanks @randall-fulton)


### v2.20.0

- back to support Go >= 1.14 (thanks @ajatprabha and @marcind)


### v2.19.0

- support for TYPE in SCAN (thanks @0xDiddi)
- update BITPOS (thanks @dirkm)
- fix a lua redis.call() return value (thanks @mpetronic)
- update ZRANGE (thanks @valdemarpereira)


### v2.18.0

- support for ZUNION (thanks @propan)
- support for COPY (thanks @matiasinsaurralde and @rockitbaby)
- support for LMOVE (thanks @btwear)


### v2.17.0

- added miniredis.RunT(t)


### v2.16.1

- fix ZINTERSTORE with wets (thanks @lingjl2010 and @okhowang)
- fix exclusive ranges in XRANGE (thanks @joseotoro)


### v2.16.0

- simplify some code (thanks @zonque)
- support for EXAT/PXAT in SET
- support for XTRIM (thanks @joseotoro)
- support for ZRANDMEMBER
- support for redis.log() in lua (thanks @dirkm)


### v2.15.2

- Fix race condition in blocking code (thanks @zonque and @robx)
- XREAD accepts '$' as ID (thanks @bradengroom)


### v2.15.1

- EVAL should cache the script (thanks @guoshimin)


### v2.15.0

- target redis 6.2 and added new args to various commands
- support for all hyperlog commands (thanks @ilbaktin)
- support for GETDEL (thanks @wszaranski)


### v2.14.5

- added XPENDING
- support for BLOCK option in XREAD and XREADGROUP


### v2.14.4

- fix BITPOS error (thanks @xiaoyuzdy)
- small fixes for XREAD, XACK, and XDEL. Mostly error cases.
- fix empty EXEC return type (thanks @ashanbrown)
- fix XDEL (thanks @svakili and @yvesf)
- fix FLUSHALL for streams (thanks @svakili)


### v2.14.3

- fix problem where Lua code didn't set the selected DB
- update to redis 6.0.10 (thanks @lazappa)


### v2.14.2

- update LUA dependency
- deal with (p)unsubscribe when there are no channels


### v2.14.1

- mod tidy


### v2.14.0

- support for HELLO and the RESP3 protocol
- KEEPTTL in SET (thanks @johnpena)


### v2.13.3

- support Go 1.14 and 1.15
- update the `Check...()` methods
- support for XREAD (thanks @pieterlexis)


### v2.13.2

- Use SAN instead of CN in self signed cert for testing (thanks @johejo)
- Travis CI now tests against the most recent two versions of Go (thanks @johejo)
- changed unit and integration tests to compare raw payloads, not parsed payloads
- remove "redigo" dependency


### v2.13.1

- added HSTRLEN
- minimal support for ACL users in AUTH


### v2.13.0

- added RunTLS(...)
- added SetError(...)


### v2.12.0

- redis 6
- Lua json update (thanks @gsmith85)
- CLUSTER commands (thanks @kratisto)
- fix TOUCH
- fix a shutdown race condition


### v2.11.4

- ZUNIONSTORE now supports standard set types (thanks @wshirey)


### v2.11.3

- support for TOUCH (thanks @cleroux)
- support for cluster and stream commands (thanks @kak-tus)


### v2.11.2

- make sure Lua code is executed concurrently
- add command GEORADIUSBYMEMBER (thanks @kyeett)


### v2.11.1

- globals protection for Lua code (thanks @vk-outreach)
- HSET update (thanks @carlgreen)
- fix BLPOP block on shutdown (thanks @Asalle)


### v2.11.0

- added XRANGE/XREVRANGE, XADD, and XLEN (thanks @skateinmars)
- added GEODIST
- improved precision for geohashes, closer to what real redis does
- use 128bit floats internally for INCRBYFLOAT and related (thanks @timnd)


### v2.10.1

- added m.Server()


### v2.10.0

- added UNLINK
- fix DEL zero-argument case
- cleanup some direct access commands
- added GEOADD, GEOPOS, GEORADIUS, and GEORADIUS_RO


### v2.9.1

- fix issue with ZRANGEBYLEX
- fix issue with BRPOPLPUSH and direct access


### v2.9.0

- proper versioned import of github.com/gomodule/redigo (thanks @yfei1)
- fix messages generated by PSUBSCRIBE
- optional internal seed (thanks @zikaeroh)


### v2.8.0

Proper `v2` in go.mod.


### older

See https://github.com/alicebob/miniredis/releases for the full changelog
