// Code generated for linux/amd64 by 'generator -D_GCC_NULLPTR_T -D_Float16=short -D__bf16=short -mlong-double-64 --package-name libsqlite3 --prefix-enumerator=_ --prefix-external=x_ --prefix-field=F --prefix-static-internal=_ --prefix-static-none=_ --prefix-tagged-enum=_ --prefix-tagged-struct=T --prefix-tagged-union=T --prefix-typename=T --prefix-undefined=_ -ignore-unsupported-alignment -ignore-link-errors -import=sync -DHAVE_USLEEP -DLONGDOUBLE_TYPE=double -DNDEBUG -DSQLITE_DEFAULT_MEMSTATUS=0 -DSQLITE_DISABLE_INTRINSIC -DSQLITE_ENABLE_COLUMN_METADATA -DSQLITE_ENABLE_DBPAGE_VTAB -DSQLITE_ENABLE_DBSTAT_VTAB -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_GEOPOLY -DSQLITE_ENABLE_JSON1 -DSQLITE_ENABLE_MATH_FUNCTIONS -DSQLITE_ENABLE_MEMORY_MANAGEMENT -DSQLITE_ENABLE_OFFSET_SQL_FUNC -DSQLITE_ENABLE_PREUPDATE_HOOK -DSQLITE_ENABLE_RBU -DSQLITE_ENABLE_RTREE -DSQLITE_ENABLE_SESSION -DSQLITE_ENABLE_SNAPSHOT -DSQLITE_ENABLE_STAT4 -DSQLITE_ENABLE_UNLOCK_NOTIFY -DSQLITE_HAVE_ZLIB=1 -DSQLITE_LIKE_DOESNT_MATCH_BLOBS -DSQLITE_SOUNDEX -DSQLITE_THREADSAFE=1 -DSQLITE_WITHOUT_ZONEMALLOC -D_LARGEFILE64_SOURCE -I /home/jnml/src/modernc.org/builder/.exclude/modernc.org/libc/include/linux/amd64 -I /home/jnml/src/modernc.org/builder/.exclude/modernc.org/libz/include/linux/amd64 -I /home/jnml/src/modernc.org/builder/.exclude/modernc.org/libtcl8.6/include/linux/amd64 -extended-errors -o sqlite3.go sqlite3.c -DSQLITE_OS_UNIX=1 -eval-all-macros', DO NOT EDIT.

//go:build linux && amd64

package sqlite3

import (
	"unsafe"

	"modernc.org/libc"
)

type Tstat = struct {
	Fst_dev     Tdev_t
	Fst_ino     Tino_t
	Fst_nlink   Tnlink_t
	Fst_mode    Tmode_t
	Fst_uid     Tuid_t
	Fst_gid     Tgid_t
	F__pad0     uint32
	Fst_rdev    Tdev_t
	Fst_size    Toff_t
	Fst_blksize Tblksize_t
	Fst_blocks  Tblkcnt_t
	Fst_atim    Ttimespec
	Fst_mtim    Ttimespec
	Fst_ctim    Ttimespec
	F__unused   [3]int64
}

// C documentation
//
//	/*
//	** Append a single path element to the DbPath under construction
//	*/
func _appendOnePathElement(tls *libc.TLS, pPath uintptr, zName uintptr, nName int32) {
	bp := tls.Alloc(4256)
	defer tls.Free(4256)
	var got Tssize_t
	var zIn, v2 uintptr
	var v1 int32
	var _ /* buf at bp+0 */ Tstat
	var _ /* zLnk at bp+144 */ [4098]int8
	_, _, _, _ = got, zIn, v1, v2
	if int32(**(**int8)(__ccgo_up(zName))) == int32('.') {
		if nName == int32(1) {
			return
		}
		if int32(**(**int8)(__ccgo_up(zName + 1))) == int32('.') && nName == int32(2) {
			if (*TDbPath)(unsafe.Pointer(pPath)).FnUsed > int32(1) {
				for {
					v2 = pPath + 20
					*(*int32)(unsafe.Pointer(v2)) = *(*int32)(unsafe.Pointer(v2)) - 1
					v1 = *(*int32)(unsafe.Pointer(v2))
					if !(int32(**(**int8)(__ccgo_up((*TDbPath)(unsafe.Pointer(pPath)).FzOut + uintptr(v1)))) != int32('/')) {
						break
					}
				}
			}
			return
		}
	}
	if (*TDbPath)(unsafe.Pointer(pPath)).FnUsed+nName+int32(2) >= (*TDbPath)(unsafe.Pointer(pPath)).FnOut {
		(*TDbPath)(unsafe.Pointer(pPath)).Frc = int32(SQLITE_ERROR)
		return
	}
	v2 = pPath + 20
	v1 = *(*int32)(unsafe.Pointer(v2))
	*(*int32)(unsafe.Pointer(v2)) = *(*int32)(unsafe.Pointer(v2)) + 1
	**(**int8)(__ccgo_up((*TDbPath)(unsafe.Pointer(pPath)).FzOut + uintptr(v1))) = int8('/')
	libc.Xmemcpy(tls, (*TDbPath)(unsafe.Pointer(pPath)).FzOut+uintptr((*TDbPath)(unsafe.Pointer(pPath)).FnUsed), zName, libc.Uint64FromInt32(nName))
	**(**int32)(__ccgo_up(pPath + 20)) += nName
	if (*TDbPath)(unsafe.Pointer(pPath)).Frc == SQLITE_OK {
		**(**int8)(__ccgo_up((*TDbPath)(unsafe.Pointer(pPath)).FzOut + uintptr((*TDbPath)(unsafe.Pointer(pPath)).FnUsed))) = 0
		zIn = (*TDbPath)(unsafe.Pointer(pPath)).FzOut
		if (*(*func(*libc.TLS, uintptr, uintptr) int32)(unsafe.Pointer(&struct{ uintptr }{_aSyscall[int32(27)].FpCurrent})))(tls, zIn, bp) != 0 {
			if **(**int32)(__ccgo_up(libc.X__errno_location(tls))) != int32(ENOENT) {
				(*TDbPath)(unsafe.Pointer(pPath)).Frc = _unixLogErrorAtLine(tls, _sqlite3CantopenError(tls, int32(47297)), __ccgo_ts+3740, zIn, int32(47297))
			}
		} else {
			if (**(**Tstat)(__ccgo_up(bp))).Fst_mode&uint32(S_IFMT) == uint32(S_IFLNK) {
				v2 = pPath + 4
				v1 = *(*int32)(unsafe.Pointer(v2))
				*(*int32)(unsafe.Pointer(v2)) = *(*int32)(unsafe.Pointer(v2)) + 1
				if v1 > int32(SQLITE_MAX_SYMLINK) {
					(*TDbPath)(unsafe.Pointer(pPath)).Frc = _sqlite3CantopenError(tls, int32(47303))
					return
				}
				got = (*(*func(*libc.TLS, uintptr, uintptr, Tsize_t) Tssize_t)(unsafe.Pointer(&struct{ uintptr }{_aSyscall[int32(26)].FpCurrent})))(tls, zIn, bp+144, libc.Uint64FromInt64(4098)-libc.Uint64FromInt32(2))
				if got <= 0 || got >= libc.Int64FromInt64(4098)-libc.Int64FromInt32(2) {
					(*TDbPath)(unsafe.Pointer(pPath)).Frc = _unixLogErrorAtLine(tls, _sqlite3CantopenError(tls, int32(47308)), __ccgo_ts+3731, zIn, int32(47308))
					return
				}
				(**(**[4098]int8)(__ccgo_up(bp + 144)))[got] = 0
				if int32((**(**[4098]int8)(__ccgo_up(bp + 144)))[0]) == int32('/') {
					(*TDbPath)(unsafe.Pointer(pPath)).FnUsed = 0
				} else {
					**(**int32)(__ccgo_up(pPath + 20)) -= nName + int32(1)
				}
				_appendAllPathElements(tls, pPath, bp+144)
			}
		}
	}
}
