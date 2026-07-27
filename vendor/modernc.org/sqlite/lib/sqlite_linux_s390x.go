// Code generated for linux/s390x by 'generator -mlong-double-64 --package-name libsqlite3 --prefix-enumerator=_ --prefix-external=x_ --prefix-field=F --prefix-static-internal=_ --prefix-static-none=_ --prefix-tagged-enum=_ --prefix-tagged-struct=T --prefix-tagged-union=T --prefix-typename=T --prefix-undefined=_ -ignore-unsupported-alignment -ignore-link-errors -import=sync -DHAVE_USLEEP -DLONGDOUBLE_TYPE=double -DNDEBUG -DSQLITE_DEFAULT_MEMSTATUS=0 -DSQLITE_DISABLE_INTRINSIC -DSQLITE_ENABLE_COLUMN_METADATA -DSQLITE_ENABLE_DBPAGE_VTAB -DSQLITE_ENABLE_DBSTAT_VTAB -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_GEOPOLY -DSQLITE_ENABLE_JSON1 -DSQLITE_ENABLE_MATH_FUNCTIONS -DSQLITE_ENABLE_MEMORY_MANAGEMENT -DSQLITE_ENABLE_OFFSET_SQL_FUNC -DSQLITE_ENABLE_PREUPDATE_HOOK -DSQLITE_ENABLE_RBU -DSQLITE_ENABLE_RTREE -DSQLITE_ENABLE_SESSION -DSQLITE_ENABLE_SNAPSHOT -DSQLITE_ENABLE_STAT4 -DSQLITE_ENABLE_UNLOCK_NOTIFY -DSQLITE_HAVE_ZLIB=1 -DSQLITE_LIKE_DOESNT_MATCH_BLOBS -DSQLITE_SOUNDEX -DSQLITE_THREADSAFE=1 -DSQLITE_WITHOUT_ZONEMALLOC -D_LARGEFILE64_SOURCE -I /home/jnml/src/modernc.org/builder/.exclude/modernc.org/libc/include/linux/s390x -I /home/jnml/src/modernc.org/builder/.exclude/modernc.org/libz/include/linux/s390x -I /home/jnml/src/modernc.org/builder/.exclude/modernc.org/libtcl8.6/include/linux/s390x -extended-errors -o sqlite3.go sqlite3.c -DSQLITE_OS_UNIX=1 -eval-all-macros', DO NOT EDIT.

//go:build linux && s390x

package sqlite3

import (
	"unsafe"

	"modernc.org/libc"
)

const BYTE_ORDER = 4321

const POSIX_FADV_DONTNEED = 6

const POSIX_FADV_NOREUSE = 7

const SQLITE_BIGENDIAN = 1

const SQLITE_BYTEORDER = 4321

const SQLITE_LITTLEENDIAN = 0

const SQLITE_UTF16NATIVE = 3

type Tstat = struct {
	Fst_dev     Tdev_t
	Fst_ino     Tino_t
	Fst_nlink   Tnlink_t
	Fst_mode    Tmode_t
	Fst_uid     Tuid_t
	Fst_gid     Tgid_t
	Fst_rdev    Tdev_t
	Fst_size    Toff_t
	Fst_atim    Ttimespec
	Fst_mtim    Ttimespec
	Fst_ctim    Ttimespec
	Fst_blksize Tblksize_t
	Fst_blocks  Tblkcnt_t
	F__unused   [3]uint64
}

func Xsqlite3_bind_text16(tls *libc.TLS, pStmt uintptr, i int32, zData uintptr, n int32, __ccgo_fp_xDel uintptr) (r int32) {
	return _bindText(tls, pStmt, i, zData, libc.Int64FromUint64(libc.Uint64FromInt32(n) & ^libc.Uint64FromInt32(1)), __ccgo_fp_xDel, uint8(SQLITE_UTF16BE))
}

func Xsqlite3_bind_text64(tls *libc.TLS, pStmt uintptr, i int32, zData uintptr, nData Tsqlite3_uint64, __ccgo_fp_xDel uintptr, enc uint8) (r int32) {
	if libc.Int32FromUint8(enc) != int32(SQLITE_UTF8) && libc.Int32FromUint8(enc) != int32(SQLITE_UTF8_ZT) {
		if libc.Int32FromUint8(enc) == int32(SQLITE_UTF16) {
			enc = uint8(SQLITE_UTF16BE)
		}
		nData = nData & ^libc.Uint64FromInt32(1)
	}
	return _bindText(tls, pStmt, i, zData, libc.Int64FromUint64(nData), __ccgo_fp_xDel, enc)
}

// C documentation
//
//	/*
//	** This routine is the same as the sqlite3_complete() routine described
//	** above, except that the parameter is required to be UTF-16 encoded, not
//	** UTF-8.
//	*/
func Xsqlite3_complete16(tls *libc.TLS, zSql uintptr) (r int32) {
	var pVal, zSql8 uintptr
	var rc int32
	_, _, _ = pVal, rc, zSql8
	rc = Xsqlite3_initialize(tls)
	if rc != 0 {
		return rc
	}
	pVal = _sqlite3ValueNew(tls, uintptr(0))
	_sqlite3ValueSetStr(tls, pVal, -int32(1), zSql, uint8(SQLITE_UTF16BE), libc.UintptrFromInt32(0))
	zSql8 = _sqlite3ValueText(tls, pVal, uint8(SQLITE_UTF8))
	if zSql8 != 0 {
		rc = Xsqlite3_complete(tls, zSql8)
	} else {
		rc = int32(SQLITE_NOMEM)
	}
	_sqlite3ValueFree(tls, pVal)
	return rc & int32(0xff)
}

/************** End of rtree.h ***********************************************/
/************** Continuing where we left off in main.c ***********************/

// C documentation
//
//	/*
//	** Register a new collation sequence with the database handle db.
//	*/
func Xsqlite3_create_collation16(tls *libc.TLS, db uintptr, zName uintptr, enc int32, pCtx uintptr, __ccgo_fp_xCompare uintptr) (r int32) {
	var rc int32
	var zName8 uintptr
	_, _ = rc, zName8
	rc = SQLITE_OK
	Xsqlite3_mutex_enter(tls, (*Tsqlite3)(unsafe.Pointer(db)).Fmutex)
	zName8 = _sqlite3Utf16to8(tls, db, zName, -int32(1), uint8(SQLITE_UTF16BE))
	if zName8 != 0 {
		rc = _createCollation(tls, db, zName8, libc.Uint8FromInt32(enc), pCtx, __ccgo_fp_xCompare, uintptr(0))
		_sqlite3DbFree(tls, db, zName8)
	}
	rc = _sqlite3ApiExit(tls, db, rc)
	Xsqlite3_mutex_leave(tls, (*Tsqlite3)(unsafe.Pointer(db)).Fmutex)
	return rc
}

func Xsqlite3_create_function16(tls *libc.TLS, db uintptr, zFunctionName uintptr, nArg int32, eTextRep int32, p uintptr, __ccgo_fp_xSFunc uintptr, __ccgo_fp_xStep uintptr, __ccgo_fp_xFinal uintptr) (r int32) {
	var rc int32
	var zFunc8 uintptr
	_, _ = rc, zFunc8
	Xsqlite3_mutex_enter(tls, (*Tsqlite3)(unsafe.Pointer(db)).Fmutex)
	zFunc8 = _sqlite3Utf16to8(tls, db, zFunctionName, -int32(1), uint8(SQLITE_UTF16BE))
	rc = _sqlite3CreateFunc(tls, db, zFunc8, nArg, eTextRep, p, __ccgo_fp_xSFunc, __ccgo_fp_xStep, __ccgo_fp_xFinal, uintptr(0), uintptr(0), uintptr(0))
	_sqlite3DbFree(tls, db, zFunc8)
	rc = _sqlite3ApiExit(tls, db, rc)
	Xsqlite3_mutex_leave(tls, (*Tsqlite3)(unsafe.Pointer(db)).Fmutex)
	return rc
}

// C documentation
//
//	/*
//	** Open a new database handle.
//	*/
func Xsqlite3_open16(tls *libc.TLS, zFilename uintptr, ppDb uintptr) (r int32) {
	var pVal, zFilename8 uintptr
	var rc int32
	var v1 Tu8
	_, _, _, _ = pVal, rc, zFilename8, v1
	**(**uintptr)(__ccgo_up(ppDb)) = uintptr(0)
	rc = Xsqlite3_initialize(tls)
	if rc != 0 {
		return rc
	}
	if zFilename == uintptr(0) {
		zFilename = __ccgo_ts + 26199
	}
	pVal = _sqlite3ValueNew(tls, uintptr(0))
	_sqlite3ValueSetStr(tls, pVal, -int32(1), zFilename, uint8(SQLITE_UTF16BE), libc.UintptrFromInt32(0))
	zFilename8 = _sqlite3ValueText(tls, pVal, uint8(SQLITE_UTF8))
	if zFilename8 != 0 {
		rc = _openDatabase(tls, zFilename8, ppDb, libc.Uint32FromInt32(libc.Int32FromInt32(SQLITE_OPEN_READWRITE)|libc.Int32FromInt32(SQLITE_OPEN_CREATE)), uintptr(0))
		if rc == SQLITE_OK && !(libc.Int32FromUint16((*TSchema)(unsafe.Pointer((**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(**(**uintptr)(__ccgo_up(ppDb)))).FaDb))).FpSchema)).FschemaFlags)&libc.Int32FromInt32(DB_SchemaLoaded) == libc.Int32FromInt32(DB_SchemaLoaded)) {
			v1 = libc.Uint8FromInt32(SQLITE_UTF16BE)
			(*Tsqlite3)(unsafe.Pointer(**(**uintptr)(__ccgo_up(ppDb)))).Fenc = v1
			(*TSchema)(unsafe.Pointer((**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(**(**uintptr)(__ccgo_up(ppDb)))).FaDb))).FpSchema)).Fenc = v1
		}
	} else {
		rc = int32(SQLITE_NOMEM)
	}
	_sqlite3ValueFree(tls, pVal)
	return rc & int32(0xff)
}

func Xsqlite3_result_error16(tls *libc.TLS, pCtx uintptr, z uintptr, n int32) {
	(*Tsqlite3_context)(unsafe.Pointer(pCtx)).FisError = int32(SQLITE_ERROR)
	_sqlite3VdbeMemSetStr(tls, (*Tsqlite3_context)(unsafe.Pointer(pCtx)).FpOut, z, int64(n), uint8(SQLITE_UTF16BE), uintptr(-libc.Int32FromInt32(1)))
}

func Xsqlite3_result_text16(tls *libc.TLS, pCtx uintptr, z uintptr, n int32, __ccgo_fp_xDel uintptr) {
	_setResultStrOrError(tls, pCtx, z, libc.Int32FromUint64(libc.Uint64FromInt32(n) & ^libc.Uint64FromInt32(1)), uint8(SQLITE_UTF16BE), __ccgo_fp_xDel)
}

func Xsqlite3_result_text64(tls *libc.TLS, pCtx uintptr, z uintptr, n Tsqlite3_uint64, __ccgo_fp_xDel uintptr, enc uint8) {
	if libc.Int32FromUint8(enc) != int32(SQLITE_UTF8) && libc.Int32FromUint8(enc) != int32(SQLITE_UTF8_ZT) {
		if libc.Int32FromUint8(enc) == int32(SQLITE_UTF16) {
			enc = uint8(SQLITE_UTF16BE)
		}
		n = n & ^libc.Uint64FromInt32(1)
	}
	if n > uint64(0x7fffffff) {
		_invokeValueDestructor(tls, z, __ccgo_fp_xDel, pCtx)
	} else {
		_setResultStrOrError(tls, pCtx, z, libc.Int32FromUint64(n), enc, __ccgo_fp_xDel)
		_sqlite3VdbeMemZeroTerminateIfAble(tls, (*Tsqlite3_context)(unsafe.Pointer(pCtx)).FpOut)
	}
}

func Xsqlite3_value_bytes16(tls *libc.TLS, pVal uintptr) (r int32) {
	return _sqlite3ValueBytes(tls, pVal, uint8(SQLITE_UTF16BE))
}

func Xsqlite3_value_text16(tls *libc.TLS, pVal uintptr) (r uintptr) {
	return _sqlite3ValueText(tls, pVal, uint8(SQLITE_UTF16BE))
}

const __ARCH__ = 9

const __BYTE_ORDER = 4321

const __BYTE_ORDER__ = 4321

const __FLOAT_WORD_ORDER__ = 4321

const __GNUC_WIDE_EXECUTION_CHARSET_NAME = "UTF-32BE"

const __s390__ = 1

const __s390x__ = 1

const __zarch__ = 1

// C documentation
//
//	/*
//	** This routine redistributes cells on the iParentIdx'th child of pParent
//	** (hereafter "the page") and up to 2 siblings so that all pages have about the
//	** same amount of free space. Usually a single sibling on either side of the
//	** page are used in the balancing, though both siblings might come from one
//	** side if the page is the first or last child of its parent. If the page
//	** has fewer than 2 siblings (something which can only happen if the page
//	** is a root page or a child of a root page) then all available siblings
//	** participate in the balancing.
//	**
//	** The number of siblings of the page might be increased or decreased by
//	** one or two in an effort to keep pages nearly full but not over full.
//	**
//	** Note that when this routine is called, some of the cells on the page
//	** might not actually be stored in MemPage.aData[]. This can happen
//	** if the page is overfull. This routine ensures that all cells allocated
//	** to the page and its siblings fit into MemPage.aData[] before returning.
//	**
//	** In the course of balancing the page and its siblings, cells may be
//	** inserted into or removed from the parent page (pParent). Doing so
//	** may cause the parent page to become overfull or underfull. If this
//	** happens, it is the responsibility of the caller to invoke the correct
//	** balancing routine to fix this problem (see the balance() routine).
//	**
//	** If this routine fails for any reason, it might leave the database
//	** in a corrupted state. So if this routine fails, the database should
//	** be rolled back.
//	**
//	** The third argument to this function, aOvflSpace, is a pointer to a
//	** buffer big enough to hold one page. If while inserting cells into the parent
//	** page (pParent) the parent page becomes overfull, this buffer is
//	** used to store the parent's overflow cells. Because this function inserts
//	** a maximum of four divider cells into the parent page, and the maximum
//	** size of a cell stored within an internal node is always less than 1/4
//	** of the page-size, the aOvflSpace[] buffer is guaranteed to be large
//	** enough for all overflow cells.
//	**
//	** If aOvflSpace is set to a null pointer, this function returns
//	** SQLITE_NOMEM.
//	*/
func _balance_nonroot(tls *libc.TLS, pParent uintptr, iParentIdx int32, aOvflSpace uintptr, isRoot int32, bBulk int32) (r1 int32) {
	bp := tls.Alloc(208)
	defer tls.Free(208)
	var aData, aSpace1, p, pBt, pCell, pCell1, pNew1, pNew2, pOld, pOld1, pOld2, pRight, pSrcEnd, pTemp, pTemp1, piCell, piEnd, v17 uintptr
	var aPgno [5]TPgno
	var apDiv [2]uintptr
	var apNew [5]uintptr
	var cntNew, cntOld [5]int32
	var cntOldNext, d, i, iB, iNew, iNew1, iOff, iOld, iOld1, iOvflSpace, iPg, iSpace1, j, k, leafData, limit, nMaxCells, nNew, nNewCell, nOld, nxDiv, pageFlags, r, sz1, sz2, szD, szLeft, szR, szRight, usableSpace, v1 int32
	var fgA, fgB, leafCorrection, maskPage, sz Tu16
	var key Tu32
	var pgnoA, pgnoB, pgnoTemp TPgno
	var szScratch Tu64
	var v13, v14 bool
	var v18 uint32
	var _ /* abDone at bp+60 */ [5]Tu8
	var _ /* apOld at bp+8 */ [3]uintptr
	var _ /* b at bp+72 */ TCellArray
	var _ /* info at bp+184 */ TCellInfo
	var _ /* pNew at bp+176 */ uintptr
	var _ /* pgno at bp+52 */ TPgno
	var _ /* rc at bp+0 */ int32
	var _ /* szNew at bp+32 */ [5]int32
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _ = aData, aPgno, aSpace1, apDiv, apNew, cntNew, cntOld, cntOldNext, d, fgA, fgB, i, iB, iNew, iNew1, iOff, iOld, iOld1, iOvflSpace, iPg, iSpace1, j, k, key, leafCorrection, leafData, limit, maskPage, nMaxCells, nNew, nNewCell, nOld, nxDiv, p, pBt, pCell, pCell1, pNew1, pNew2, pOld, pOld1, pOld2, pRight, pSrcEnd, pTemp, pTemp1, pageFlags, pgnoA, pgnoB, pgnoTemp, piCell, piEnd, r, sz, sz1, sz2, szD, szLeft, szR, szRight, szScratch, usableSpace, v1, v13, v14, v17, v18 /* The whole database */
	nMaxCells = 0                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                /* Allocated size of apCell, szCell, aFrom. */
	nNew = 0                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     /* Next divider slot in pParent->aCell[] */
	**(**int32)(__ccgo_up(bp)) = SQLITE_OK                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       /* Value of pPage->aData[0] */
	iSpace1 = 0                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  /* First unused byte of aSpace1[] */
	iOvflSpace = 0                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               /* Parsed information on cells being balanced */
	libc.Xmemset(tls, bp+60, 0, uint64(5))
	libc.Xmemset(tls, bp+72, 0, libc.Uint64FromInt64(104)-libc.Uint64FromInt64(4))
	**(**int32)(__ccgo_up(bp + 72 + 80 + uintptr(libc.Int32FromInt32(NB)*libc.Int32FromInt32(2)-libc.Int32FromInt32(1))*4)) = int32(0x7fffffff)
	pBt = (*TMemPage)(unsafe.Pointer(pParent)).FpBt
	/* At this point pParent may have at most one overflow cell. And if
	 ** this overflow cell is present, it must be the cell with
	 ** index iParentIdx. This scenario comes about when this function
	 ** is called (indirectly) from sqlite3BtreeDelete().
	 */
	if !(aOvflSpace != 0) {
		return int32(SQLITE_NOMEM)
	}
	/* Find the sibling pages to balance. Also locate the cells in pParent
	 ** that divide the siblings. An attempt is made to find NN siblings on
	 ** either side of pPage. More siblings are taken from one side, however,
	 ** if there are fewer than NN siblings on the other side. If pParent
	 ** has NB or fewer children then all children of pParent are taken.
	 **
	 ** This loop also drops the divider cells from the parent page. This
	 ** way, the remainder of the function does not have to deal with any
	 ** overflow cells in the parent page, since if any existed they will
	 ** have already been removed.
	 */
	i = libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pParent)).FnOverflow) + libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pParent)).FnCell)
	if i < int32(2) {
		nxDiv = 0
	} else {
		if iParentIdx == 0 {
			nxDiv = 0
		} else {
			if iParentIdx == i {
				nxDiv = i - int32(2) + bBulk
			} else {
				nxDiv = iParentIdx - int32(1)
			}
		}
		i = int32(2) - bBulk
	}
	nOld = i + int32(1)
	if i+nxDiv-libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pParent)).FnOverflow) == libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pParent)).FnCell) {
		pRight = (*TMemPage)(unsafe.Pointer(pParent)).FaData + uintptr(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pParent)).FhdrOffset)+int32(8))
	} else {
		pRight = (*TMemPage)(unsafe.Pointer(pParent)).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pParent)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pParent)).FaCellIdx + uintptr(int32(2)*(i+nxDiv-libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pParent)).FnOverflow)))))))
	}
	**(**TPgno)(__ccgo_up(bp + 52)) = _sqlite3Get4byte(tls, pRight)
	for int32(1) != 0 {
		if **(**int32)(__ccgo_up(bp)) == SQLITE_OK {
			**(**int32)(__ccgo_up(bp)) = _getAndInitPage(tls, pBt, **(**TPgno)(__ccgo_up(bp + 52)), bp+8+uintptr(i)*8, 0)
		}
		if **(**int32)(__ccgo_up(bp)) != 0 {
			libc.Xmemset(tls, bp+8, 0, libc.Uint64FromInt32(i+libc.Int32FromInt32(1))*uint64(8))
			goto balance_cleanup
		}
		if (*TMemPage)(unsafe.Pointer((**(**[3]uintptr)(__ccgo_up(bp + 8)))[i])).FnFree < 0 {
			**(**int32)(__ccgo_up(bp)) = _btreeComputeFreeSpace(tls, (**(**[3]uintptr)(__ccgo_up(bp + 8)))[i])
			if **(**int32)(__ccgo_up(bp)) != 0 {
				libc.Xmemset(tls, bp+8, 0, libc.Uint64FromInt32(i)*uint64(8))
				goto balance_cleanup
			}
		}
		nMaxCells = nMaxCells + (libc.Int32FromUint16((*TMemPage)(unsafe.Pointer((**(**[3]uintptr)(__ccgo_up(bp + 8)))[i])).FnCell) + libc.Int32FromUint64(libc.Uint64FromInt64(32)/libc.Uint64FromInt64(8)))
		v1 = i
		i = i - 1
		if v1 == 0 {
			break
		}
		if (*TMemPage)(unsafe.Pointer(pParent)).FnOverflow != 0 && i+nxDiv == libc.Int32FromUint16(**(**Tu16)(__ccgo_up(pParent + 28))) {
			apDiv[i] = **(**uintptr)(__ccgo_up(pParent + 40))
			**(**TPgno)(__ccgo_up(bp + 52)) = _sqlite3Get4byte(tls, apDiv[i])
			(**(**[5]int32)(__ccgo_up(bp + 32)))[i] = libc.Int32FromUint16((*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pParent)).FxCellSize})))(tls, pParent, apDiv[i]))
			(*TMemPage)(unsafe.Pointer(pParent)).FnOverflow = uint8(0)
		} else {
			apDiv[i] = (*TMemPage)(unsafe.Pointer(pParent)).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pParent)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pParent)).FaCellIdx + uintptr(int32(2)*(i+nxDiv-libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pParent)).FnOverflow)))))))
			**(**TPgno)(__ccgo_up(bp + 52)) = _sqlite3Get4byte(tls, apDiv[i])
			(**(**[5]int32)(__ccgo_up(bp + 32)))[i] = libc.Int32FromUint16((*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pParent)).FxCellSize})))(tls, pParent, apDiv[i]))
			/* Drop the cell from the parent page. apDiv[i] still points to
			 ** the cell within the parent, even though it has been dropped.
			 ** This is safe because dropping a cell only overwrites the first
			 ** four bytes of it, and this function does not need the first
			 ** four bytes of the divider cell. So the pointer is safe to use
			 ** later on.
			 **
			 ** But not if we are in secure-delete mode. In secure-delete mode,
			 ** the dropCell() routine will overwrite the entire cell with zeroes.
			 ** In this case, temporarily copy the cell into the aOvflSpace[]
			 ** buffer. It will be copied out again as soon as the aSpace[] buffer
			 ** is allocated.  */
			if libc.Int32FromUint16((*TBtShared)(unsafe.Pointer(pBt)).FbtsFlags)&int32(BTS_FAST_SECURE) != 0 {
				/* If the following if() condition is not true, the db is corrupted.
				 ** The call to dropCell() below will detect this.  */
				iOff = int32(int64(apDiv[i])) - int32(int64((*TMemPage)(unsafe.Pointer(pParent)).FaData))
				if iOff+(**(**[5]int32)(__ccgo_up(bp + 32)))[i] <= libc.Int32FromUint32((*TBtShared)(unsafe.Pointer(pBt)).FusableSize) {
					libc.Xmemcpy(tls, aOvflSpace+uintptr(iOff), apDiv[i], libc.Uint64FromInt32((**(**[5]int32)(__ccgo_up(bp + 32)))[i]))
					apDiv[i] = aOvflSpace + uintptr(int64(apDiv[i])-int64((*TMemPage)(unsafe.Pointer(pParent)).FaData))
				}
			}
			_dropCell(tls, pParent, i+nxDiv-libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pParent)).FnOverflow), (**(**[5]int32)(__ccgo_up(bp + 32)))[i], bp)
		}
	}
	/* Make nMaxCells a multiple of 4 in order to preserve 8-byte
	 ** alignment */
	nMaxCells = (nMaxCells + int32(3)) & ^libc.Int32FromInt32(3)
	/*
	 ** Allocate space for memory structures
	 */
	szScratch = uint64(libc.Uint64FromInt32(nMaxCells)*uint64(8) + libc.Uint64FromInt32(nMaxCells)*uint64(2) + uint64((*TBtShared)(unsafe.Pointer(pBt)).FpageSize)) /* aSpace1 */
	(**(**TCellArray)(__ccgo_up(bp + 72))).FapCell = _sqlite3DbMallocRaw(tls, uintptr(0), szScratch)
	if (**(**TCellArray)(__ccgo_up(bp + 72))).FapCell == uintptr(0) {
		**(**int32)(__ccgo_up(bp)) = int32(SQLITE_NOMEM)
		goto balance_cleanup
	}
	(**(**TCellArray)(__ccgo_up(bp + 72))).FszCell = (**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr(nMaxCells)*8
	aSpace1 = (**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr(nMaxCells)*2
	/*
	 ** Load pointers to all cells on sibling pages and the divider cells
	 ** into the local b.apCell[] array.  Make copies of the divider cells
	 ** into space obtained from aSpace1[]. The divider cells have already
	 ** been removed from pParent.
	 **
	 ** If the siblings are on leaf pages, then the child pointers of the
	 ** divider cells are stripped from the cells before they are copied
	 ** into aSpace1[].  In this way, all cells in b.apCell[] are without
	 ** child pointers.  If siblings are not leaves, then all cell in
	 ** b.apCell[] include child pointers.  Either way, all cells in b.apCell[]
	 ** are alike.
	 **
	 ** leafCorrection:  4 if pPage is a leaf.  0 if pPage is not a leaf.
	 **       leafData:  1 if pPage holds key+data and pParent holds only keys.
	 */
	(**(**TCellArray)(__ccgo_up(bp + 72))).FpRef = (**(**[3]uintptr)(__ccgo_up(bp + 8)))[0]
	leafCorrection = libc.Uint16FromInt32(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer((**(**TCellArray)(__ccgo_up(bp + 72))).FpRef)).Fleaf) * int32(4))
	leafData = libc.Int32FromUint8((*TMemPage)(unsafe.Pointer((**(**TCellArray)(__ccgo_up(bp + 72))).FpRef)).FintKeyLeaf)
	i = 0
	for {
		if !(i < nOld) {
			break
		}
		pOld = (**(**[3]uintptr)(__ccgo_up(bp + 8)))[i]
		limit = libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pOld)).FnCell)
		aData = (*TMemPage)(unsafe.Pointer(pOld)).FaData
		maskPage = (*TMemPage)(unsafe.Pointer(pOld)).FmaskPage
		piCell = aData + uintptr((*TMemPage)(unsafe.Pointer(pOld)).FcellOffset)
		/* Verify that all sibling pages are of the same "type" (table-leaf,
		 ** table-interior, index-leaf, or index-interior).
		 */
		if libc.Int32FromUint8(**(**Tu8)(__ccgo_up((*TMemPage)(unsafe.Pointer(pOld)).FaData))) != libc.Int32FromUint8(**(**Tu8)(__ccgo_up((*TMemPage)(unsafe.Pointer((**(**[3]uintptr)(__ccgo_up(bp + 8)))[0])).FaData))) {
			**(**int32)(__ccgo_up(bp)) = _sqlite3CorruptError(tls, int32(81575))
			goto balance_cleanup
		}
		/* Load b.apCell[] with pointers to all cells in pOld.  If pOld
		 ** contains overflow cells, include them in the b.apCell[] array
		 ** in the correct spot.
		 **
		 ** Note that when there are multiple overflow cells, it is always the
		 ** case that they are sequential and adjacent.  This invariant arises
		 ** because multiple overflows can only occurs when inserting divider
		 ** cells into a parent on a prior balance, and divider cells are always
		 ** adjacent and are inserted in order.  There is an assert() tagged
		 ** with "NOTE 1" in the overflow cell insertion loop to prove this
		 ** invariant.
		 **
		 ** This must be done in advance.  Once the balance starts, the cell
		 ** offset section of the btree page will be overwritten and we will no
		 ** long be able to find the cells if a pointer to each cell is not saved
		 ** first.
		 */
		libc.Xmemset(tls, (**(**TCellArray)(__ccgo_up(bp + 72))).FszCell+uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*2, 0, uint64(2)*libc.Uint64FromInt32(limit+libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pOld)).FnOverflow)))
		if libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pOld)).FnOverflow) > 0 {
			if limit < libc.Int32FromUint16(**(**Tu16)(__ccgo_up(pOld + 28))) {
				**(**int32)(__ccgo_up(bp)) = _sqlite3CorruptError(tls, int32(81599))
				goto balance_cleanup
			}
			limit = libc.Int32FromUint16(**(**Tu16)(__ccgo_up(pOld + 28)))
			j = 0
			for {
				if !(j < limit) {
					break
				}
				**(**uintptr)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*8)) = aData + uintptr(libc.Int32FromUint16(maskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up(piCell))))
				piCell = piCell + uintptr(2)
				(**(**TCellArray)(__ccgo_up(bp + 72))).FnCell = (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell + 1
				goto _3
			_3:
				;
				j = j + 1
			}
			k = 0
			for {
				if !(k < libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pOld)).FnOverflow)) {
					break
				}
				/* NOTE 1 */
				**(**uintptr)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*8)) = **(**uintptr)(__ccgo_up(pOld + 40 + uintptr(k)*8))
				(**(**TCellArray)(__ccgo_up(bp + 72))).FnCell = (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell + 1
				goto _4
			_4:
				;
				k = k + 1
			}
		}
		piEnd = aData + uintptr((*TMemPage)(unsafe.Pointer(pOld)).FcellOffset) + uintptr(int32(2)*libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pOld)).FnCell))
		for piCell < piEnd {
			**(**uintptr)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*8)) = aData + uintptr(libc.Int32FromUint16(maskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up(piCell))))
			piCell = piCell + uintptr(2)
			(**(**TCellArray)(__ccgo_up(bp + 72))).FnCell = (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell + 1
		}
		cntOld[i] = (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell
		if i < nOld-int32(1) && !(leafData != 0) {
			sz = libc.Uint16FromInt32((**(**[5]int32)(__ccgo_up(bp + 32)))[i])
			**(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*2)) = sz
			pTemp = aSpace1 + uintptr(iSpace1)
			iSpace1 = iSpace1 + libc.Int32FromUint16(sz)
			libc.Xmemcpy(tls, pTemp, apDiv[i], uint64(sz))
			**(**uintptr)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*8)) = pTemp + uintptr(leafCorrection)
			**(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*2)) = libc.Uint16FromInt32(libc.Int32FromUint16(**(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*2))) - libc.Int32FromUint16(leafCorrection))
			if !((*TMemPage)(unsafe.Pointer(pOld)).Fleaf != 0) {
				/* The right pointer of the child page pOld becomes the left
				 ** pointer of the divider cell */
				libc.Xmemcpy(tls, **(**uintptr)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*8)), (*TMemPage)(unsafe.Pointer(pOld)).FaData+8, uint64(4))
			} else {
				for libc.Int32FromUint16(**(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*2))) < int32(4) {
					/* Do not allow any cells smaller than 4 bytes. If a smaller cell
					 ** does exist, pad it with 0x00 bytes. */
					v1 = iSpace1
					iSpace1 = iSpace1 + 1
					**(**Tu8)(__ccgo_up(aSpace1 + uintptr(v1))) = uint8(0x00)
					**(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*2)) = **(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr((**(**TCellArray)(__ccgo_up(bp + 72))).FnCell)*2)) + 1
				}
			}
			(**(**TCellArray)(__ccgo_up(bp + 72))).FnCell = (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell + 1
		}
		goto _2
	_2:
		;
		i = i + 1
	}
	/*
	 ** Figure out the number of pages needed to hold all b.nCell cells.
	 ** Store this number in "k".  Also compute szNew[] which is the total
	 ** size of all cells on the i-th page and cntNew[] which is the index
	 ** in b.apCell[] of the cell that divides page i from page i+1.
	 ** cntNew[k] should equal b.nCell.
	 **
	 ** Values computed by this block:
	 **
	 **           k: The total number of sibling pages
	 **    szNew[i]: Spaced used on the i-th sibling page.
	 **   cntNew[i]: Index in b.apCell[] and b.szCell[] for the first cell to
	 **              the right of the i-th sibling page.
	 ** usableSpace: Number of bytes of space available on each sibling.
	 **
	 */
	usableSpace = libc.Int32FromUint32((*TBtShared)(unsafe.Pointer(pBt)).FusableSize - uint32(12) + uint32(leafCorrection))
	v1 = libc.Int32FromInt32(0)
	k = v1
	i = v1
	for {
		if !(i < nOld) {
			break
		}
		p = (**(**[3]uintptr)(__ccgo_up(bp + 8)))[i]
		**(**uintptr)(__ccgo_up(bp + 72 + 32 + uintptr(k)*8)) = (*TMemPage)(unsafe.Pointer(p)).FaDataEnd
		**(**int32)(__ccgo_up(bp + 72 + 80 + uintptr(k)*4)) = cntOld[i]
		if k != 0 && **(**int32)(__ccgo_up(bp + 72 + 80 + uintptr(k)*4)) == **(**int32)(__ccgo_up(bp + 72 + 80 + uintptr(k-int32(1))*4)) {
			k = k - 1 /* Omit b.ixNx[] entry for child pages with no cells */
		}
		if !(leafData != 0) {
			k = k + 1
			**(**uintptr)(__ccgo_up(bp + 72 + 32 + uintptr(k)*8)) = (*TMemPage)(unsafe.Pointer(pParent)).FaDataEnd
			**(**int32)(__ccgo_up(bp + 72 + 80 + uintptr(k)*4)) = cntOld[i] + int32(1)
		}
		(**(**[5]int32)(__ccgo_up(bp + 32)))[i] = usableSpace - (*TMemPage)(unsafe.Pointer(p)).FnFree
		j = 0
		for {
			if !(j < libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(p)).FnOverflow)) {
				break
			}
			**(**int32)(__ccgo_up(bp + 32 + uintptr(i)*4)) += int32(2) + libc.Int32FromUint16((*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(p)).FxCellSize})))(tls, p, **(**uintptr)(__ccgo_up(p + 40 + uintptr(j)*8))))
			goto _8
		_8:
			;
			j = j + 1
		}
		cntNew[i] = cntOld[i]
		goto _6
	_6:
		;
		i = i + 1
		k = k + 1
	}
	k = nOld
	i = 0
	for {
		if !(i < k) {
			break
		}
		for (**(**[5]int32)(__ccgo_up(bp + 32)))[i] > usableSpace {
			if i+int32(1) >= k {
				k = i + int32(2)
				if k > libc.Int32FromInt32(NB)+libc.Int32FromInt32(2) {
					**(**int32)(__ccgo_up(bp)) = _sqlite3CorruptError(tls, int32(81700))
					goto balance_cleanup
				}
				(**(**[5]int32)(__ccgo_up(bp + 32)))[k-int32(1)] = 0
				cntNew[k-int32(1)] = (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell
			}
			sz1 = int32(2) + libc.Int32FromUint16(_cachedCellSize(tls, bp+72, cntNew[i]-int32(1)))
			**(**int32)(__ccgo_up(bp + 32 + uintptr(i)*4)) -= sz1
			if !(leafData != 0) {
				if cntNew[i] < (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell {
					sz1 = int32(2) + libc.Int32FromUint16(_cachedCellSize(tls, bp+72, cntNew[i]))
				} else {
					sz1 = 0
				}
			}
			**(**int32)(__ccgo_up(bp + 32 + uintptr(i+int32(1))*4)) += sz1
			cntNew[i] = cntNew[i] - 1
		}
		for cntNew[i] < (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell {
			sz1 = int32(2) + libc.Int32FromUint16(_cachedCellSize(tls, bp+72, cntNew[i]))
			if (**(**[5]int32)(__ccgo_up(bp + 32)))[i]+sz1 > usableSpace {
				break
			}
			**(**int32)(__ccgo_up(bp + 32 + uintptr(i)*4)) += sz1
			cntNew[i] = cntNew[i] + 1
			if !(leafData != 0) {
				if cntNew[i] < (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell {
					sz1 = int32(2) + libc.Int32FromUint16(_cachedCellSize(tls, bp+72, cntNew[i]))
				} else {
					sz1 = 0
				}
			}
			**(**int32)(__ccgo_up(bp + 32 + uintptr(i+int32(1))*4)) -= sz1
		}
		if cntNew[i] >= (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell {
			k = i + int32(1)
		} else {
			if i > 0 {
				v1 = cntNew[i-int32(1)]
			} else {
				v1 = 0
			}
			if cntNew[i] <= v1 {
				**(**int32)(__ccgo_up(bp)) = _sqlite3CorruptError(tls, int32(81733))
				goto balance_cleanup
			}
		}
		goto _9
	_9:
		;
		i = i + 1
	}
	/*
	 ** The packing computed by the previous block is biased toward the siblings
	 ** on the left side (siblings with smaller keys). The left siblings are
	 ** always nearly full, while the right-most sibling might be nearly empty.
	 ** The next block of code attempts to adjust the packing of siblings to
	 ** get a better balance.
	 **
	 ** This adjustment is more than an optimization.  The packing above might
	 ** be so out of balance as to be illegal.  For example, the right-most
	 ** sibling might be completely empty.  This adjustment is not optional.
	 */
	i = k - int32(1)
	for {
		if !(i > 0) {
			break
		}
		szRight = (**(**[5]int32)(__ccgo_up(bp + 32)))[i]         /* Size of sibling on the right */
		szLeft = (**(**[5]int32)(__ccgo_up(bp + 32)))[i-int32(1)] /* Index of first cell to the left of right sibling */
		r = cntNew[i-int32(1)] - int32(1)
		d = r + int32(1) - leafData
		_cachedCellSize(tls, bp+72, d)
		for cond := true; cond; cond = r >= 0 {
			szR = libc.Int32FromUint16(_cachedCellSize(tls, bp+72, r))
			szD = libc.Int32FromUint16(**(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr(d)*2)))
			if v14 = szRight != 0; v14 {
				if v13 = bBulk != 0; !v13 {
					if i == k-int32(1) {
						v1 = 0
					} else {
						v1 = int32(2)
					}
				}
			}
			if v14 && (v13 || szRight+szD+int32(2) > szLeft-(szR+v1)) {
				break
			}
			szRight = szRight + (szD + int32(2))
			szLeft = szLeft - (szR + int32(2))
			cntNew[i-int32(1)] = r
			r = r - 1
			d = d - 1
		}
		(**(**[5]int32)(__ccgo_up(bp + 32)))[i] = szRight
		(**(**[5]int32)(__ccgo_up(bp + 32)))[i-int32(1)] = szLeft
		if i > int32(1) {
			v1 = cntNew[i-int32(2)]
		} else {
			v1 = 0
		}
		if cntNew[i-int32(1)] <= v1 {
			**(**int32)(__ccgo_up(bp)) = _sqlite3CorruptError(tls, int32(81777))
			goto balance_cleanup
		}
		goto _11
	_11:
		;
		i = i - 1
	}
	/* Sanity check:  For a non-corrupt database file one of the following
	 ** must be true:
	 **    (1) We found one or more cells (cntNew[0])>0), or
	 **    (2) pPage is a virtual root page.  A virtual root page is when
	 **        the real root page is page 1 and we are the only child of
	 **        that page.
	 */
	/*
	 ** Allocate k new pages.  Reuse old pages where possible.
	 */
	pageFlags = libc.Int32FromUint8(**(**Tu8)(__ccgo_up((*TMemPage)(unsafe.Pointer((**(**[3]uintptr)(__ccgo_up(bp + 8)))[0])).FaData)))
	i = 0
	for {
		if !(i < k) {
			break
		}
		if i < nOld {
			v17 = (**(**[3]uintptr)(__ccgo_up(bp + 8)))[i]
			apNew[i] = v17
			**(**uintptr)(__ccgo_up(bp + 176)) = v17
			(**(**[3]uintptr)(__ccgo_up(bp + 8)))[i] = uintptr(0)
			**(**int32)(__ccgo_up(bp)) = _sqlite3PagerWrite(tls, (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 176)))).FpDbPage)
			nNew = nNew + 1
			if _sqlite3PagerPageRefcount(tls, (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 176)))).FpDbPage) != int32(1)+libc.BoolInt32(i == iParentIdx-nxDiv) && **(**int32)(__ccgo_up(bp)) == SQLITE_OK {
				**(**int32)(__ccgo_up(bp)) = _sqlite3CorruptError(tls, int32(81810))
			}
			if **(**int32)(__ccgo_up(bp)) != 0 {
				goto balance_cleanup
			}
		} else {
			if bBulk != 0 {
				v18 = uint32(1)
			} else {
				v18 = **(**TPgno)(__ccgo_up(bp + 52))
			}
			**(**int32)(__ccgo_up(bp)) = _allocateBtreePage(tls, pBt, bp+176, bp+52, v18, uint8(0))
			if **(**int32)(__ccgo_up(bp)) != 0 {
				goto balance_cleanup
			}
			_zeroPage(tls, **(**uintptr)(__ccgo_up(bp + 176)), pageFlags)
			apNew[i] = **(**uintptr)(__ccgo_up(bp + 176))
			nNew = nNew + 1
			cntOld[i] = (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell
			/* Set the pointer-map entry for the new sibling page. */
			if (*TBtShared)(unsafe.Pointer(pBt)).FautoVacuum != 0 {
				_ptrmapPut(tls, pBt, (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 176)))).Fpgno, uint8(PTRMAP_BTREE), (*TMemPage)(unsafe.Pointer(pParent)).Fpgno, bp)
				if **(**int32)(__ccgo_up(bp)) != SQLITE_OK {
					goto balance_cleanup
				}
			}
		}
		goto _16
	_16:
		;
		i = i + 1
	}
	/*
	 ** Reassign page numbers so that the new pages are in ascending order.
	 ** This helps to keep entries in the disk file in order so that a scan
	 ** of the table is closer to a linear scan through the file. That in turn
	 ** helps the operating system to deliver pages from the disk more rapidly.
	 **
	 ** An O(N*N) sort algorithm is used, but since N is never more than NB+2
	 ** (5), that is not a performance concern.
	 **
	 ** When NB==3, this one optimization makes the database about 25% faster
	 ** for large insertions and deletions.
	 */
	i = 0
	for {
		if !(i < nNew) {
			break
		}
		aPgno[i] = (*TMemPage)(unsafe.Pointer(apNew[i])).Fpgno
		goto _19
	_19:
		;
		i = i + 1
	}
	i = 0
	for {
		if !(i < nNew-int32(1)) {
			break
		}
		iB = i
		j = i + int32(1)
		for {
			if !(j < nNew) {
				break
			}
			if (*TMemPage)(unsafe.Pointer(apNew[j])).Fpgno < (*TMemPage)(unsafe.Pointer(apNew[iB])).Fpgno {
				iB = j
			}
			goto _21
		_21:
			;
			j = j + 1
		}
		/* If apNew[i] has a page number that is bigger than any of the
		 ** subsequence apNew[i] entries, then swap apNew[i] with the subsequent
		 ** entry that has the smallest page number (which we know to be
		 ** entry apNew[iB]).
		 */
		if iB != i {
			pgnoA = (*TMemPage)(unsafe.Pointer(apNew[i])).Fpgno
			pgnoB = (*TMemPage)(unsafe.Pointer(apNew[iB])).Fpgno
			pgnoTemp = libc.Uint32FromInt32(_sqlite3PendingByte)/(*TBtShared)(unsafe.Pointer(pBt)).FpageSize + uint32(1)
			fgA = (*TDbPage)(unsafe.Pointer((*TMemPage)(unsafe.Pointer(apNew[i])).FpDbPage)).Fflags
			fgB = (*TDbPage)(unsafe.Pointer((*TMemPage)(unsafe.Pointer(apNew[iB])).FpDbPage)).Fflags
			_sqlite3PagerRekey(tls, (*TMemPage)(unsafe.Pointer(apNew[i])).FpDbPage, pgnoTemp, fgB)
			_sqlite3PagerRekey(tls, (*TMemPage)(unsafe.Pointer(apNew[iB])).FpDbPage, pgnoA, fgA)
			_sqlite3PagerRekey(tls, (*TMemPage)(unsafe.Pointer(apNew[i])).FpDbPage, pgnoB, fgB)
			(*TMemPage)(unsafe.Pointer(apNew[i])).Fpgno = pgnoB
			(*TMemPage)(unsafe.Pointer(apNew[iB])).Fpgno = pgnoA
		}
		goto _20
	_20:
		;
		i = i + 1
	}
	_sqlite3Put4byte(tls, pRight, (*TMemPage)(unsafe.Pointer(apNew[nNew-int32(1)])).Fpgno)
	/* If the sibling pages are not leaves, ensure that the right-child pointer
	 ** of the right-most new sibling page is set to the value that was
	 ** originally in the same field of the right-most old sibling page. */
	if pageFlags&int32(PTF_LEAF) == 0 && nOld != nNew {
		if nNew > nOld {
			pOld1 = apNew[nOld-int32(1)]
		} else {
			pOld1 = (**(**[3]uintptr)(__ccgo_up(bp + 8)))[nOld-int32(1)]
		}
		libc.Xmemcpy(tls, (*TMemPage)(unsafe.Pointer(apNew[nNew-int32(1)])).FaData+8, (*TMemPage)(unsafe.Pointer(pOld1)).FaData+8, uint64(4))
	}
	/* Make any required updates to pointer map entries associated with
	 ** cells stored on sibling pages following the balance operation. Pointer
	 ** map entries associated with divider cells are set by the insertCell()
	 ** routine. The associated pointer map entries are:
	 **
	 **   a) if the cell contains a reference to an overflow chain, the
	 **      entry associated with the first page in the overflow chain, and
	 **
	 **   b) if the sibling pages are not leaves, the child page associated
	 **      with the cell.
	 **
	 ** If the sibling pages are not leaves, then the pointer map entry
	 ** associated with the right-child of each sibling may also need to be
	 ** updated. This happens below, after the sibling pages have been
	 ** populated, not here.
	 */
	if (*TBtShared)(unsafe.Pointer(pBt)).FautoVacuum != 0 {
		v17 = apNew[0]
		pOld2 = v17
		pNew1 = v17
		cntOldNext = libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pNew1)).FnCell) + libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pNew1)).FnOverflow)
		iNew = 0
		iOld = 0
		i = 0
		for {
			if !(i < (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell) {
				break
			}
			pCell = **(**uintptr)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr(i)*8))
			for i == cntOldNext {
				iOld = iOld + 1
				if iOld < nNew {
					v17 = apNew[iOld]
				} else {
					v17 = (**(**[3]uintptr)(__ccgo_up(bp + 8)))[iOld]
				}
				pOld2 = v17
				cntOldNext = cntOldNext + (libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pOld2)).FnCell) + libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pOld2)).FnOverflow) + libc.BoolInt32(!(leafData != 0)))
			}
			if i == cntNew[iNew] {
				iNew = iNew + 1
				v1 = iNew
				pNew1 = apNew[v1]
				if !(leafData != 0) {
					goto _23
				}
			}
			/* Cell pCell is destined for new sibling page pNew. Originally, it
			 ** was either part of sibling page iOld (possibly an overflow cell),
			 ** or else the divider cell to the left of sibling page iOld. So,
			 ** if sibling page iOld had the same page number as pNew, and if
			 ** pCell really was a part of sibling page iOld (not a divider or
			 ** overflow cell), we can skip updating the pointer map entries.  */
			if iOld >= nNew || (*TMemPage)(unsafe.Pointer(pNew1)).Fpgno != aPgno[iOld] || !(uint64(pCell) >= uint64((*TMemPage)(unsafe.Pointer(pOld2)).FaData) && uint64(pCell) < uint64((*TMemPage)(unsafe.Pointer(pOld2)).FaDataEnd)) {
				if !(leafCorrection != 0) {
					_ptrmapPut(tls, pBt, _sqlite3Get4byte(tls, pCell), uint8(PTRMAP_BTREE), (*TMemPage)(unsafe.Pointer(pNew1)).Fpgno, bp)
				}
				if libc.Int32FromUint16(_cachedCellSize(tls, bp+72, i)) > libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pNew1)).FminLocal) {
					_ptrmapPutOvflPtr(tls, pNew1, pOld2, pCell, bp)
				}
				if **(**int32)(__ccgo_up(bp)) != 0 {
					goto balance_cleanup
				}
			}
			goto _23
		_23:
			;
			i = i + 1
		}
	}
	/* Insert new divider cells into pParent. */
	i = 0
	for {
		if !(i < nNew-int32(1)) {
			break
		}
		pNew2 = apNew[i]
		j = cntNew[i]
		pCell1 = **(**uintptr)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr(j)*8))
		sz2 = libc.Int32FromUint16(**(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr(j)*2))) + libc.Int32FromUint16(leafCorrection)
		pTemp1 = aOvflSpace + uintptr(iOvflSpace)
		if !((*TMemPage)(unsafe.Pointer(pNew2)).Fleaf != 0) {
			libc.Xmemcpy(tls, (*TMemPage)(unsafe.Pointer(pNew2)).FaData+8, pCell1, uint64(4))
		} else {
			if leafData != 0 {
				j = j - 1
				(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pNew2)).FxParseCell})))(tls, pNew2, **(**uintptr)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FapCell + uintptr(j)*8)), bp+184)
				pCell1 = pTemp1
				sz2 = int32(4) + _sqlite3PutVarint(tls, pCell1+4, libc.Uint64FromInt64((**(**TCellInfo)(__ccgo_up(bp + 184))).FnKey))
				pTemp1 = uintptr(0)
			} else {
				pCell1 = pCell1 - uintptr(4)
				/* Obscure case for non-leaf-data trees: If the cell at pCell was
				 ** previously stored on a leaf node, and its reported size was 4
				 ** bytes, then it may actually be smaller than this
				 ** (see btreeParseCellPtr(), 4 bytes is the minimum size of
				 ** any cell). But it is important to pass the correct size to
				 ** insertCell(), so reparse the cell now.
				 **
				 ** This can only happen for b-trees used to evaluate "IN (SELECT ...)"
				 ** and WITHOUT ROWID tables with exactly one column which is the
				 ** primary key.
				 */
				if libc.Int32FromUint16(**(**Tu16)(__ccgo_up((**(**TCellArray)(__ccgo_up(bp + 72))).FszCell + uintptr(j)*2))) == int32(4) {
					sz2 = libc.Int32FromUint16((*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pParent)).FxCellSize})))(tls, pParent, pCell1))
				}
			}
		}
		iOvflSpace = iOvflSpace + sz2
		k = 0
		for {
			if !(**(**int32)(__ccgo_up(bp + 72 + 80 + uintptr(k)*4)) <= j) {
				break
			}
			goto _27
		_27:
			;
			k = k + 1
		}
		pSrcEnd = **(**uintptr)(__ccgo_up(bp + 72 + 32 + uintptr(k)*8))
		if uint64(pCell1) < uint64(pSrcEnd) && uint64(pCell1+uintptr(sz2)) > uint64(pSrcEnd) {
			**(**int32)(__ccgo_up(bp)) = _sqlite3CorruptError(tls, int32(82016))
			goto balance_cleanup
		}
		**(**int32)(__ccgo_up(bp)) = _insertCell(tls, pParent, nxDiv+i, pCell1, sz2, pTemp1, (*TMemPage)(unsafe.Pointer(pNew2)).Fpgno)
		if **(**int32)(__ccgo_up(bp)) != SQLITE_OK {
			goto balance_cleanup
		}
		goto _26
	_26:
		;
		i = i + 1
	}
	/* Now update the actual sibling pages. The order in which they are updated
	 ** is important, as this code needs to avoid disrupting any page from which
	 ** cells may still to be read. In practice, this means:
	 **
	 **  (1) If cells are moving left (from apNew[iPg] to apNew[iPg-1])
	 **      then it is not safe to update page apNew[iPg] until after
	 **      the left-hand sibling apNew[iPg-1] has been updated.
	 **
	 **  (2) If cells are moving right (from apNew[iPg] to apNew[iPg+1])
	 **      then it is not safe to update page apNew[iPg] until after
	 **      the right-hand sibling apNew[iPg+1] has been updated.
	 **
	 ** If neither of the above apply, the page is safe to update.
	 **
	 ** The iPg value in the following loop starts at nNew-1 goes down
	 ** to 0, then back up to nNew-1 again, thus making two passes over
	 ** the pages.  On the initial downward pass, only condition (1) above
	 ** needs to be tested because (2) will always be true from the previous
	 ** step.  On the upward pass, both conditions are always true, so the
	 ** upwards pass simply processes pages that were missed on the downward
	 ** pass.
	 */
	i = int32(1) - nNew
	for {
		if !(i < nNew) {
			break
		}
		if i < 0 {
			v1 = -i
		} else {
			v1 = i
		}
		iPg = v1
		if (**(**[5]Tu8)(__ccgo_up(bp + 60)))[iPg] != 0 {
			goto _28
		} /* Skip pages already processed */
		if i >= 0 || cntOld[iPg-int32(1)] >= cntNew[iPg-int32(1)] {
			/* Verify condition (1):  If cells are moving left, update iPg
			 ** only after iPg-1 has already been updated. */
			/* Verify condition (2):  If cells are moving right, update iPg
			 ** only after iPg+1 has already been updated. */
			if iPg == 0 {
				v1 = libc.Int32FromInt32(0)
				iOld1 = v1
				iNew1 = v1
				nNewCell = cntNew[0]
			} else {
				if iPg < nOld {
					v1 = cntOld[iPg-int32(1)] + libc.BoolInt32(!(leafData != 0))
				} else {
					v1 = (**(**TCellArray)(__ccgo_up(bp + 72))).FnCell
				}
				iOld1 = v1
				iNew1 = cntNew[iPg-int32(1)] + libc.BoolInt32(!(leafData != 0))
				nNewCell = cntNew[iPg] - iNew1
			}
			**(**int32)(__ccgo_up(bp)) = _editPage(tls, apNew[iPg], iOld1, iNew1, nNewCell, bp+72)
			if **(**int32)(__ccgo_up(bp)) != 0 {
				goto balance_cleanup
			}
			(**(**[5]Tu8)(__ccgo_up(bp + 60)))[iPg] = (**(**[5]Tu8)(__ccgo_up(bp + 60)))[iPg] + 1
			(*TMemPage)(unsafe.Pointer(apNew[iPg])).FnFree = usableSpace - (**(**[5]int32)(__ccgo_up(bp + 32)))[iPg]
		}
		goto _28
	_28:
		;
		i = i + 1
	}
	/* All pages have been processed exactly once */
	if isRoot != 0 && libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pParent)).FnCell) == 0 && libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pParent)).FhdrOffset) <= (*TMemPage)(unsafe.Pointer(apNew[0])).FnFree {
		/* The root page of the b-tree now contains no cells. The only sibling
		 ** page is the right-child of the parent. Copy the contents of the
		 ** child page into the parent, decreasing the overall height of the
		 ** b-tree structure by one. This is described as the "balance-shallower"
		 ** sub-algorithm in some documentation.
		 **
		 ** If this is an auto-vacuum database, the call to copyNodeContent()
		 ** sets all pointer-map entries corresponding to database image pages
		 ** for which the pointer is stored within the content being copied.
		 **
		 ** It is critical that the child page be defragmented before being
		 ** copied into the parent, because if the parent is page 1 then it will
		 ** by smaller than the child due to the database header, and so all the
		 ** free space needs to be up front.
		 */
		**(**int32)(__ccgo_up(bp)) = _defragmentPage(tls, apNew[0], -int32(1))
		_copyNodeContent(tls, apNew[0], pParent, bp)
		_freePage(tls, apNew[0], bp)
	} else {
		if (*TBtShared)(unsafe.Pointer(pBt)).FautoVacuum != 0 && !(leafCorrection != 0) {
			/* Fix the pointer map entries associated with the right-child of each
			 ** sibling page. All other pointer map entries have already been taken
			 ** care of.  */
			i = 0
			for {
				if !(i < nNew) {
					break
				}
				key = _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(apNew[i])).FaData+8)
				_ptrmapPut(tls, pBt, key, uint8(PTRMAP_BTREE), (*TMemPage)(unsafe.Pointer(apNew[i])).Fpgno, bp)
				goto _32
			_32:
				;
				i = i + 1
			}
		}
	}
	/* Free any old pages that were not reused as new pages.
	 */
	i = nNew
	for {
		if !(i < nOld) {
			break
		}
		_freePage(tls, (**(**[3]uintptr)(__ccgo_up(bp + 8)))[i], bp)
		goto _33
	_33:
		;
		i = i + 1
	}
	/*
	 ** Cleanup before returning.
	 */
	goto balance_cleanup
balance_cleanup:
	;
	_sqlite3DbFree(tls, uintptr(0), (**(**TCellArray)(__ccgo_up(bp + 72))).FapCell)
	i = 0
	for {
		if !(i < nOld) {
			break
		}
		_releasePage(tls, (**(**[3]uintptr)(__ccgo_up(bp + 8)))[i])
		goto _34
	_34:
		;
		i = i + 1
	}
	i = 0
	for {
		if !(i < nNew) {
			break
		}
		_releasePage(tls, apNew[i])
		goto _35
	_35:
		;
		i = i + 1
	}
	return **(**int32)(__ccgo_up(bp))
}

// C documentation
//
//	/*
//	** This version of balance() handles the common special case where
//	** a new entry is being inserted on the extreme right-end of the
//	** tree, in other words, when the new entry will become the largest
//	** entry in the tree.
//	**
//	** Instead of trying to balance the 3 right-most leaf pages, just add
//	** a new page to the right-hand side and put the one new entry in
//	** that page.  This leaves the right side of the tree somewhat
//	** unbalanced.  But odds are that we will be inserting new entries
//	** at the end soon afterwards so the nearly empty page will quickly
//	** fill up.  On average.
//	**
//	** pPage is the leaf page which is the right-most page in the tree.
//	** pParent is its parent.  pPage must have a single overflow entry
//	** which is also the right-most entry on the page.
//	**
//	** The pSpace buffer is used to store a temporary copy of the divider
//	** cell that will be inserted into pParent. Such a cell consists of a 4
//	** byte page number followed by a variable length integer. In other
//	** words, at most 13 bytes. Hence the pSpace buffer must be at
//	** least 13 bytes in size.
//	*/
func _balance_quick(tls *libc.TLS, pParent uintptr, pPage uintptr, pSpace uintptr) (r int32) {
	bp := tls.Alloc(144)
	defer tls.Free(144)
	var pBt, pOut, pStop, v1, v3 uintptr
	var v2 Tu8
	var _ /* b at bp+32 */ TCellArray
	var _ /* pCell at bp+16 */ uintptr
	var _ /* pNew at bp+0 */ uintptr
	var _ /* pgnoNew at bp+12 */ TPgno
	var _ /* rc at bp+8 */ int32
	var _ /* szCell at bp+24 */ Tu16
	_, _, _, _, _, _ = pBt, pOut, pStop, v1, v2, v3
	pBt = (*TMemPage)(unsafe.Pointer(pPage)).FpBt /* Page number of pNew */
	if libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) == 0 {
		return _sqlite3CorruptError(tls, int32(81151))
	} /* dbfuzz001.test */
	/* Allocate a new page. This page will become the right-sibling of
	 ** pPage. Make the parent page writable, so that the new divider cell
	 ** may be inserted. If both these operations are successful, proceed.
	 */
	**(**int32)(__ccgo_up(bp + 8)) = _allocateBtreePage(tls, pBt, bp, bp+12, uint32(0), uint8(0))
	if **(**int32)(__ccgo_up(bp + 8)) == SQLITE_OK {
		pOut = pSpace + 4
		**(**uintptr)(__ccgo_up(bp + 16)) = **(**uintptr)(__ccgo_up(pPage + 40))
		**(**Tu16)(__ccgo_up(bp + 24)) = (*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxCellSize})))(tls, pPage, **(**uintptr)(__ccgo_up(bp + 16)))
		_zeroPage(tls, **(**uintptr)(__ccgo_up(bp)), libc.Int32FromInt32(PTF_INTKEY)|libc.Int32FromInt32(PTF_LEAFDATA)|libc.Int32FromInt32(PTF_LEAF))
		(**(**TCellArray)(__ccgo_up(bp + 32))).FnCell = int32(1)
		(**(**TCellArray)(__ccgo_up(bp + 32))).FpRef = pPage
		(**(**TCellArray)(__ccgo_up(bp + 32))).FapCell = bp + 16
		(**(**TCellArray)(__ccgo_up(bp + 32))).FszCell = bp + 24
		**(**uintptr)(__ccgo_up(bp + 32 + 32)) = (*TMemPage)(unsafe.Pointer(pPage)).FaDataEnd
		**(**int32)(__ccgo_up(bp + 32 + 80)) = int32(2)
		**(**int32)(__ccgo_up(bp + 32 + 80 + uintptr(libc.Int32FromInt32(NB)*libc.Int32FromInt32(2)-libc.Int32FromInt32(1))*4)) = int32(0x7fffffff)
		**(**int32)(__ccgo_up(bp + 8)) = _rebuildPage(tls, bp+32, 0, int32(1), **(**uintptr)(__ccgo_up(bp)))
		if **(**int32)(__ccgo_up(bp + 8)) != 0 {
			_releasePage(tls, **(**uintptr)(__ccgo_up(bp)))
			return **(**int32)(__ccgo_up(bp + 8))
		}
		(*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FnFree = libc.Int32FromUint32((*TBtShared)(unsafe.Pointer(pBt)).FusableSize - uint32((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FcellOffset) - uint32(2) - uint32(**(**Tu16)(__ccgo_up(bp + 24))))
		/* If this is an auto-vacuum database, update the pointer map
		 ** with entries for the new page, and any pointer from the
		 ** cell on the page to an overflow page. If either of these
		 ** operations fails, the return code is set, but the contents
		 ** of the parent page are still manipulated by the code below.
		 ** That is Ok, at this point the parent page is guaranteed to
		 ** be marked as dirty. Returning an error code will cause a
		 ** rollback, undoing any changes made to the parent page.
		 */
		if (*TBtShared)(unsafe.Pointer(pBt)).FautoVacuum != 0 {
			_ptrmapPut(tls, pBt, **(**TPgno)(__ccgo_up(bp + 12)), uint8(PTRMAP_BTREE), (*TMemPage)(unsafe.Pointer(pParent)).Fpgno, bp+8)
			if libc.Int32FromUint16(**(**Tu16)(__ccgo_up(bp + 24))) > libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FminLocal) {
				_ptrmapPutOvflPtr(tls, **(**uintptr)(__ccgo_up(bp)), **(**uintptr)(__ccgo_up(bp)), **(**uintptr)(__ccgo_up(bp + 16)), bp+8)
			}
		}
		/* Create a divider cell to insert into pParent. The divider cell
		 ** consists of a 4-byte page number (the page number of pPage) and
		 ** a variable length key value (which must be the same value as the
		 ** largest key on pPage).
		 **
		 ** To find the largest key value on pPage, first find the right-most
		 ** cell on pPage. The first two fields of this cell are the
		 ** record-length (a variable length integer at most 32-bits in size)
		 ** and the key value (a variable length integer, may have any value).
		 ** The first of the while(...) loops below skips over the record-length
		 ** field. The second while(...) loop copies the key value from the
		 ** cell on pPage into the pSpace buffer.
		 */
		**(**uintptr)(__ccgo_up(bp + 16)) = (*TMemPage)(unsafe.Pointer(pPage)).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell)-int32(1)))))))
		pStop = **(**uintptr)(__ccgo_up(bp + 16)) + 9
		for {
			v1 = **(**uintptr)(__ccgo_up(bp + 16))
			**(**uintptr)(__ccgo_up(bp + 16)) = **(**uintptr)(__ccgo_up(bp + 16)) + 1
			if !(libc.Int32FromUint8(**(**Tu8)(__ccgo_up(v1)))&int32(0x80) != 0 && **(**uintptr)(__ccgo_up(bp + 16)) < pStop) {
				break
			}
		}
		pStop = **(**uintptr)(__ccgo_up(bp + 16)) + 9
		for {
			v1 = **(**uintptr)(__ccgo_up(bp + 16))
			**(**uintptr)(__ccgo_up(bp + 16)) = **(**uintptr)(__ccgo_up(bp + 16)) + 1
			v2 = **(**Tu8)(__ccgo_up(v1))
			v3 = pOut
			pOut = pOut + 1
			**(**Tu8)(__ccgo_up(v3)) = v2
			if !(libc.Int32FromUint8(v2)&int32(0x80) != 0 && **(**uintptr)(__ccgo_up(bp + 16)) < pStop) {
				break
			}
		}
		/* Insert the new divider cell into pParent. */
		if **(**int32)(__ccgo_up(bp + 8)) == SQLITE_OK {
			**(**int32)(__ccgo_up(bp + 8)) = _insertCell(tls, pParent, libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pParent)).FnCell), pSpace, int32(int64(pOut)-int64(pSpace)), uintptr(0), (*TMemPage)(unsafe.Pointer(pPage)).Fpgno)
		}
		/* Set the right-child pointer of pParent to point to the new page. */
		_sqlite3Put4byte(tls, (*TMemPage)(unsafe.Pointer(pParent)).FaData+uintptr(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pParent)).FhdrOffset)+int32(8)), **(**TPgno)(__ccgo_up(bp + 12)))
		/* Release the reference to the new page. */
		_releasePage(tls, **(**uintptr)(__ccgo_up(bp)))
	}
	return **(**int32)(__ccgo_up(bp + 8))
}

// C documentation
//
//	/*
//	** Do additional sanity check after btreeInitPage() if
//	** PRAGMA cell_size_check=ON
//	*/
func _btreeCellSizeCheck(tls *libc.TLS, pPage uintptr) (r int32) {
	var cellOffset, i, iCellFirst, iCellLast, pc, sz, usableSize int32
	var data uintptr
	_, _, _, _, _, _, _, _ = cellOffset, data, i, iCellFirst, iCellLast, pc, sz, usableSize /* Start of cell content area */
	iCellFirst = libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FcellOffset) + int32(2)*libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell)
	usableSize = libc.Int32FromUint32((*TBtShared)(unsafe.Pointer((*TMemPage)(unsafe.Pointer(pPage)).FpBt)).FusableSize)
	iCellLast = usableSize - int32(4)
	data = (*TMemPage)(unsafe.Pointer(pPage)).FaData
	cellOffset = libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FcellOffset)
	if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
		iCellLast = iCellLast - 1
	}
	i = 0
	for {
		if !(i < libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell)) {
			break
		}
		pc = libc.Int32FromUint16(**(**Tu16)(__ccgo_up(data + uintptr(cellOffset+i*int32(2)))))
		if pc < iCellFirst || pc > iCellLast {
			return _sqlite3CorruptError(tls, int32(75335))
		}
		sz = libc.Int32FromUint16((*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxCellSize})))(tls, pPage, data+uintptr(pc)))
		if pc+sz > usableSize {
			return _sqlite3CorruptError(tls, int32(75340))
		}
		goto _1
	_1:
		;
		i = i + 1
	}
	return SQLITE_OK
}

func _btreeParseCell(tls *libc.TLS, pPage uintptr, iCell int32, pInfo uintptr) {
	(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxParseCell})))(tls, pPage, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*iCell))))), pInfo)
}

// C documentation
//
//	/*
//	** Step the cursor to the back to the previous entry in the database.
//	** Return values:
//	**
//	**     SQLITE_OK     success
//	**     SQLITE_DONE   the cursor is already on the first element of the table
//	**     otherwise     some kind of error occurred
//	**
//	** The main entry point is sqlite3BtreePrevious().  That routine is optimized
//	** for the common case of merely decrementing the cell counter BtCursor.aiIdx
//	** to the previous cell on the current page.  The (slower) btreePrevious()
//	** helper routine is called when it is necessary to move to a different page
//	** or to restore the cursor.
//	**
//	** If bit 0x01 of the F argument to sqlite3BtreePrevious(C,F) is 1, then
//	** the cursor corresponds to an SQL index and this routine could have been
//	** skipped if the SQL index had been a unique index.  The F argument is a
//	** hint to the implement.  The native SQLite btree implementation does not
//	** use this hint, but COMDB2 does.
//	*/
func _btreePrevious(tls *libc.TLS, pCur uintptr) (r int32) {
	var idx, rc, v1 int32
	var pPage uintptr
	_, _, _, _ = idx, pPage, rc, v1
	if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) != CURSOR_VALID {
		if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) >= int32(CURSOR_REQUIRESEEK) {
			v1 = _btreeRestoreCursorPosition(tls, pCur)
		} else {
			v1 = SQLITE_OK
		}
		rc = v1
		if rc != SQLITE_OK {
			return rc
		}
		if int32(CURSOR_INVALID) == libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) {
			return int32(SQLITE_DONE)
		}
		if int32(CURSOR_SKIPNEXT) == libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) {
			(*TBtCursor)(unsafe.Pointer(pCur)).FeState = uint8(CURSOR_VALID)
			if (*TBtCursor)(unsafe.Pointer(pCur)).FskipNext < 0 {
				return SQLITE_OK
			}
		}
	}
	pPage = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
	if _sqlite3FaultSim(tls, int32(412)) != 0 {
		(*TMemPage)(unsafe.Pointer(pPage)).FisInit = uint8(0)
	}
	if !((*TMemPage)(unsafe.Pointer(pPage)).FisInit != 0) {
		return _sqlite3CorruptError(tls, int32(79582))
	}
	if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
		idx = libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix)
		rc = _moveToChild(tls, pCur, _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*idx)))))))
		if rc != 0 {
			return rc
		}
		rc = _moveToRightmost(tls, pCur)
	} else {
		for libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix) == 0 {
			if int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage) == 0 {
				(*TBtCursor)(unsafe.Pointer(pCur)).FeState = uint8(CURSOR_INVALID)
				return int32(SQLITE_DONE)
			}
			_moveToParent(tls, pCur)
		}
		(*TBtCursor)(unsafe.Pointer(pCur)).Fix = (*TBtCursor)(unsafe.Pointer(pCur)).Fix - 1
		pPage = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
		if (*TMemPage)(unsafe.Pointer(pPage)).FintKey != 0 && !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
			rc = _sqlite3BtreePrevious(tls, pCur, 0)
		} else {
			rc = SQLITE_OK
		}
	}
	return rc
}

// C documentation
//
//	/*
//	** Invoke the 'collation needed' callback to request a collation sequence
//	** in the encoding enc of name zName, length nName.
//	*/
func _callCollNeeded(tls *libc.TLS, db uintptr, enc int32, zName uintptr) {
	var pTmp, zExternal, zExternal1 uintptr
	_, _, _ = pTmp, zExternal, zExternal1
	if (*Tsqlite3)(unsafe.Pointer(db)).FxCollNeeded != 0 {
		zExternal = _sqlite3DbStrDup(tls, db, zName)
		if !(zExternal != 0) {
			return
		}
		(*(*func(*libc.TLS, uintptr, uintptr, int32, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*Tsqlite3)(unsafe.Pointer(db)).FxCollNeeded})))(tls, (*Tsqlite3)(unsafe.Pointer(db)).FpCollNeededArg, db, enc, zExternal)
		_sqlite3DbFree(tls, db, zExternal)
	}
	if (*Tsqlite3)(unsafe.Pointer(db)).FxCollNeeded16 != 0 {
		pTmp = _sqlite3ValueNew(tls, db)
		_sqlite3ValueSetStr(tls, pTmp, -int32(1), zName, uint8(SQLITE_UTF8), libc.UintptrFromInt32(0))
		zExternal1 = _sqlite3ValueText(tls, pTmp, uint8(SQLITE_UTF16BE))
		if zExternal1 != 0 {
			(*(*func(*libc.TLS, uintptr, uintptr, int32, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*Tsqlite3)(unsafe.Pointer(db)).FxCollNeeded16})))(tls, (*Tsqlite3)(unsafe.Pointer(db)).FpCollNeededArg, db, libc.Int32FromUint8((*Tsqlite3)(unsafe.Pointer(db)).Fenc), zExternal1)
		}
		_sqlite3ValueFree(tls, pTmp)
	}
}

// C documentation
//
//	/*
//	** Do various sanity checks on a single page of a tree.  Return
//	** the tree depth.  Root pages return 0.  Parents of root pages
//	** return 1, and so forth.
//	**
//	** These checks are done:
//	**
//	**      1.  Make sure that cells and freeblocks do not overlap
//	**          but combine to completely cover the page.
//	**      2.  Make sure integer cell keys are in order.
//	**      3.  Check the integrity of overflow pages.
//	**      4.  Recursively call checkTreePage on all children.
//	**      5.  Verify that the depth of all children is the same.
//	*/
func _checkTreePage(tls *libc.TLS, pCheck uintptr, iPage TPgno, piMinKey uintptr, _maxKey Ti64) (r int32) {
	bp := tls.Alloc(80)
	defer tls.Free(80)
	*(*Ti64)(unsafe.Pointer(bp)) = _maxKey
	var cellStart, d2, depth, doCoverageCheck, hdr, i, j, keyCanBeEqual, nCell, nFrag, pgno, rc, saved_v1, saved_v2, size1, v1 int32
	var contentOffset, nPage, pc, prev, size, usableSize Tu32
	var data, heap, pBt, pCell, pCellIdx, saved_zPfx uintptr
	var pgnoOvfl TPgno
	var savedIsInit Tu8
	var _ /* info at bp+24 */ TCellInfo
	var _ /* pPage at bp+8 */ uintptr
	var _ /* x at bp+16 */ Tu32
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _ = cellStart, contentOffset, d2, data, depth, doCoverageCheck, hdr, heap, i, j, keyCanBeEqual, nCell, nFrag, nPage, pBt, pCell, pCellIdx, pc, pgno, pgnoOvfl, prev, rc, savedIsInit, saved_v1, saved_v2, saved_zPfx, size, size1, usableSize, v1
	**(**uintptr)(__ccgo_up(bp + 8)) = uintptr(0) /* Result code from subroutine call */
	depth = -int32(1)                             /* Number of cells */
	doCoverageCheck = int32(1)                    /* True if cell coverage checking should be done */
	keyCanBeEqual = int32(1)                      /* Offset to the start of the cell content area */
	heap = uintptr(0)
	prev = uint32(0) /* Next and previous entry on the min-heap */
	saved_zPfx = (*TIntegrityCk)(unsafe.Pointer(pCheck)).FzPfx
	saved_v1 = libc.Int32FromUint32((*TIntegrityCk)(unsafe.Pointer(pCheck)).Fv1)
	saved_v2 = (*TIntegrityCk)(unsafe.Pointer(pCheck)).Fv2
	savedIsInit = uint8(0)
	/* Check that the page exists
	 */
	_checkProgress(tls, pCheck)
	if (*TIntegrityCk)(unsafe.Pointer(pCheck)).FmxErr == 0 {
		goto end_of_check
	}
	pBt = (*TIntegrityCk)(unsafe.Pointer(pCheck)).FpBt
	usableSize = (*TBtShared)(unsafe.Pointer(pBt)).FusableSize
	if iPage == uint32(0) {
		return 0
	}
	if _checkRef(tls, pCheck, iPage) != 0 {
		return 0
	}
	(*TIntegrityCk)(unsafe.Pointer(pCheck)).FzPfx = __ccgo_ts + 4598
	(*TIntegrityCk)(unsafe.Pointer(pCheck)).Fv1 = iPage
	v1 = _btreeGetPage(tls, pBt, iPage, bp+8, 0)
	rc = v1
	if v1 != 0 {
		_checkAppendMsg(tls, pCheck, __ccgo_ts+4616, libc.VaList(bp+56, rc))
		if rc == libc.Int32FromInt32(SQLITE_IOERR)|libc.Int32FromInt32(12)<<libc.Int32FromInt32(8) {
			(*TIntegrityCk)(unsafe.Pointer(pCheck)).Frc = int32(SQLITE_NOMEM)
		}
		goto end_of_check
	}
	/* Clear MemPage.isInit to make sure the corruption detection code in
	 ** btreeInitPage() is executed.  */
	savedIsInit = (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FisInit
	(*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FisInit = uint8(0)
	v1 = _btreeInitPage(tls, **(**uintptr)(__ccgo_up(bp + 8)))
	rc = v1
	if v1 != 0 {
		/* The only possible error from InitPage */
		_checkAppendMsg(tls, pCheck, __ccgo_ts+4654, libc.VaList(bp+56, rc))
		goto end_of_check
	}
	v1 = _btreeComputeFreeSpace(tls, **(**uintptr)(__ccgo_up(bp + 8)))
	rc = v1
	if v1 != 0 {
		_checkAppendMsg(tls, pCheck, __ccgo_ts+4692, libc.VaList(bp+56, rc))
		goto end_of_check
	}
	data = (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FaData
	hdr = libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FhdrOffset)
	/* Set up for cell analysis */
	(*TIntegrityCk)(unsafe.Pointer(pCheck)).FzPfx = __ccgo_ts + 4714
	contentOffset = libc.Uint32FromInt32((libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(hdr+int32(5)))))<<libc.Int32FromInt32(8)|libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(hdr+int32(5)) + 1)))-libc.Int32FromInt32(1))&libc.Int32FromInt32(0xffff) + libc.Int32FromInt32(1))
	/* Enforced by btreeInitPage() */
	/* EVIDENCE-OF: R-37002-32774 The two-byte integer at offset 3 gives the
	 ** number of cells on the page. */
	nCell = libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(hdr+int32(3)))))<<int32(8) | libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(hdr+int32(3)) + 1)))
	if (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).Fleaf != 0 || libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FintKey) == 0 {
		**(**Ti64)(__ccgo_up(pCheck + 120)) += int64(nCell)
	}
	/* EVIDENCE-OF: R-23882-45353 The cell pointer array of a b-tree page
	 ** immediately follows the b-tree page header. */
	cellStart = hdr + int32(12) - int32(4)*libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).Fleaf)
	pCellIdx = data + uintptr(cellStart+int32(2)*(nCell-int32(1)))
	if !((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).Fleaf != 0) {
		/* Analyze the right-child page of internal pages */
		pgno = libc.Int32FromUint32(_sqlite3Get4byte(tls, data+uintptr(hdr+int32(8))))
		if (*TBtShared)(unsafe.Pointer(pBt)).FautoVacuum != 0 {
			(*TIntegrityCk)(unsafe.Pointer(pCheck)).FzPfx = __ccgo_ts + 4740
			_checkPtrmap(tls, pCheck, libc.Uint32FromInt32(pgno), uint8(PTRMAP_BTREE), iPage)
		}
		depth = _checkTreePage(tls, pCheck, libc.Uint32FromInt32(pgno), bp, **(**Ti64)(__ccgo_up(bp)))
		keyCanBeEqual = 0
	} else {
		/* For leaf pages, the coverage check will occur in the same loop
		 ** as the other cell checks, so initialize the heap.  */
		heap = (*TIntegrityCk)(unsafe.Pointer(pCheck)).Fheap
		**(**Tu32)(__ccgo_up(heap)) = uint32(0)
	}
	/* EVIDENCE-OF: R-02776-14802 The cell pointer array consists of K 2-byte
	 ** integer offsets to the cell contents. */
	i = nCell - int32(1)
	for {
		if !(i >= 0 && (*TIntegrityCk)(unsafe.Pointer(pCheck)).FmxErr != 0) {
			break
		}
		/* Check cell size */
		(*TIntegrityCk)(unsafe.Pointer(pCheck)).Fv2 = i
		pc = uint32(**(**Tu16)(__ccgo_up(pCellIdx)))
		pCellIdx = pCellIdx - uintptr(2)
		if pc < contentOffset || pc > usableSize-uint32(4) {
			_checkAppendMsg(tls, pCheck, __ccgo_ts+4770, libc.VaList(bp+56, pc, contentOffset, usableSize-uint32(4)))
			doCoverageCheck = 0
			goto _4
		}
		pCell = data + uintptr(pc)
		(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FxParseCell})))(tls, **(**uintptr)(__ccgo_up(bp + 8)), pCell, bp+24)
		if pc+uint32((**(**TCellInfo)(__ccgo_up(bp + 24))).FnSize) > usableSize {
			_checkAppendMsg(tls, pCheck, __ccgo_ts+4800, 0)
			doCoverageCheck = 0
			goto _4
		}
		/* Check for integer primary key out of range */
		if (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FintKey != 0 {
			if keyCanBeEqual != 0 {
				v1 = libc.BoolInt32((**(**TCellInfo)(__ccgo_up(bp + 24))).FnKey > **(**Ti64)(__ccgo_up(bp)))
			} else {
				v1 = libc.BoolInt32((**(**TCellInfo)(__ccgo_up(bp + 24))).FnKey >= **(**Ti64)(__ccgo_up(bp)))
			}
			if v1 != 0 {
				_checkAppendMsg(tls, pCheck, __ccgo_ts+4824, libc.VaList(bp+56, (**(**TCellInfo)(__ccgo_up(bp + 24))).FnKey))
			}
			**(**Ti64)(__ccgo_up(bp)) = (**(**TCellInfo)(__ccgo_up(bp + 24))).FnKey
			keyCanBeEqual = 0 /* Only the first key on the page may ==maxKey */
		}
		/* Check the content overflow list */
		if (**(**TCellInfo)(__ccgo_up(bp + 24))).FnPayload > uint32((**(**TCellInfo)(__ccgo_up(bp + 24))).FnLocal) { /* First page of the overflow chain */
			nPage = ((**(**TCellInfo)(__ccgo_up(bp + 24))).FnPayload - uint32((**(**TCellInfo)(__ccgo_up(bp + 24))).FnLocal) + usableSize - uint32(5)) / (usableSize - uint32(4))
			pgnoOvfl = _sqlite3Get4byte(tls, pCell+uintptr(libc.Int32FromUint16((**(**TCellInfo)(__ccgo_up(bp + 24))).FnSize)-int32(4)))
			if (*TBtShared)(unsafe.Pointer(pBt)).FautoVacuum != 0 {
				_checkPtrmap(tls, pCheck, pgnoOvfl, uint8(PTRMAP_OVERFLOW1), iPage)
			}
			_checkList(tls, pCheck, 0, pgnoOvfl, nPage)
		}
		if !((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).Fleaf != 0) {
			/* Check sanity of left child page for internal pages */
			pgno = libc.Int32FromUint32(_sqlite3Get4byte(tls, pCell))
			if (*TBtShared)(unsafe.Pointer(pBt)).FautoVacuum != 0 {
				_checkPtrmap(tls, pCheck, libc.Uint32FromInt32(pgno), uint8(PTRMAP_BTREE), iPage)
			}
			d2 = _checkTreePage(tls, pCheck, libc.Uint32FromInt32(pgno), bp, **(**Ti64)(__ccgo_up(bp)))
			keyCanBeEqual = 0
			if d2 != depth {
				_checkAppendMsg(tls, pCheck, __ccgo_ts+4848, 0)
				depth = d2
			}
		} else {
			/* Populate the coverage-checking heap for leaf pages */
			_btreeHeapInsert(tls, heap, pc<<libc.Int32FromInt32(16)|(pc+uint32((**(**TCellInfo)(__ccgo_up(bp + 24))).FnSize)-uint32(1)))
		}
		goto _4
	_4:
		;
		i = i - 1
	}
	**(**Ti64)(__ccgo_up(piMinKey)) = **(**Ti64)(__ccgo_up(bp))
	/* Check for complete coverage of the page
	 */
	(*TIntegrityCk)(unsafe.Pointer(pCheck)).FzPfx = uintptr(0)
	if doCoverageCheck != 0 && (*TIntegrityCk)(unsafe.Pointer(pCheck)).FmxErr > 0 {
		/* For leaf pages, the min-heap has already been initialized and the
		 ** cells have already been inserted.  But for internal pages, that has
		 ** not yet been done, so do it now */
		if !((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).Fleaf != 0) {
			heap = (*TIntegrityCk)(unsafe.Pointer(pCheck)).Fheap
			**(**Tu32)(__ccgo_up(heap)) = uint32(0)
			i = nCell - int32(1)
			for {
				if !(i >= 0) {
					break
				}
				pc = uint32(**(**Tu16)(__ccgo_up(data + uintptr(cellStart+i*int32(2)))))
				size = uint32((*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FxCellSize})))(tls, **(**uintptr)(__ccgo_up(bp + 8)), data+uintptr(pc)))
				_btreeHeapInsert(tls, heap, pc<<libc.Int32FromInt32(16)|(pc+size-uint32(1)))
				goto _6
			_6:
				;
				i = i - 1
			}
		}
		/* Add the freeblocks to the min-heap
		 **
		 ** EVIDENCE-OF: R-20690-50594 The second field of the b-tree page header
		 ** is the offset of the first freeblock, or zero if there are no
		 ** freeblocks on the page.
		 */
		i = libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(hdr+int32(1)))))<<int32(8) | libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(hdr+int32(1)) + 1)))
		for i > 0 {
			/* Enforced by btreeComputeFreeSpace() */
			size1 = libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(i+int32(2)))))<<int32(8) | libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(i+int32(2)) + 1)))
			/* due to btreeComputeFreeSpace() */
			_btreeHeapInsert(tls, heap, libc.Uint32FromInt32(i)<<libc.Int32FromInt32(16)|libc.Uint32FromInt32(i+size1-libc.Int32FromInt32(1)))
			/* EVIDENCE-OF: R-58208-19414 The first 2 bytes of a freeblock are a
			 ** big-endian integer which is the offset in the b-tree page of the next
			 ** freeblock in the chain, or zero if the freeblock is the last on the
			 ** chain. */
			j = libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(i))))<<int32(8) | libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(i) + 1)))
			/* EVIDENCE-OF: R-06866-39125 Freeblocks are always connected in order of
			 ** increasing offset. */
			/* Enforced by btreeComputeFreeSpace() */
			/* Enforced by btreeComputeFreeSpace() */
			i = j
		}
		/* Analyze the min-heap looking for overlap between cells and/or
		 ** freeblocks, and counting the number of untracked bytes in nFrag.
		 **
		 ** Each min-heap entry is of the form:    (start_address<<16)|end_address.
		 ** There is an implied first entry the covers the page header, the cell
		 ** pointer index, and the gap between the cell pointer index and the start
		 ** of cell content.
		 **
		 ** The loop below pulls entries from the min-heap in order and compares
		 ** the start_address against the previous end_address.  If there is an
		 ** overlap, that means bytes are used multiple times.  If there is a gap,
		 ** that gap is added to the fragmentation count.
		 */
		nFrag = 0
		prev = contentOffset - uint32(1) /* Implied first min-heap entry */
		for _btreeHeapPull(tls, heap, bp+16) != 0 {
			if prev&uint32(0xffff) >= **(**Tu32)(__ccgo_up(bp + 16))>>libc.Int32FromInt32(16) {
				_checkAppendMsg(tls, pCheck, __ccgo_ts+4873, libc.VaList(bp+56, **(**Tu32)(__ccgo_up(bp + 16))>>int32(16), iPage))
				break
			} else {
				nFrag = libc.Int32FromUint32(uint32(nFrag) + (**(**Tu32)(__ccgo_up(bp + 16))>>libc.Int32FromInt32(16) - prev&libc.Uint32FromInt32(0xffff) - libc.Uint32FromInt32(1)))
				prev = **(**Tu32)(__ccgo_up(bp + 16))
			}
		}
		nFrag = libc.Int32FromUint32(uint32(nFrag) + (usableSize - prev&libc.Uint32FromInt32(0xffff) - libc.Uint32FromInt32(1)))
		/* EVIDENCE-OF: R-43263-13491 The total number of bytes in all fragments
		 ** is stored in the fifth field of the b-tree page header.
		 ** EVIDENCE-OF: R-07161-27322 The one-byte integer at offset 7 gives the
		 ** number of fragmented free bytes within the cell content area.
		 */
		if **(**Tu32)(__ccgo_up(heap)) == uint32(0) && nFrag != libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(hdr+int32(7))))) {
			_checkAppendMsg(tls, pCheck, __ccgo_ts+4910, libc.VaList(bp+56, nFrag, libc.Int32FromUint8(**(**Tu8)(__ccgo_up(data + uintptr(hdr+int32(7))))), iPage))
		}
	}
	goto end_of_check
end_of_check:
	;
	if !(doCoverageCheck != 0) {
		(*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 8)))).FisInit = savedIsInit
	}
	_releasePage(tls, **(**uintptr)(__ccgo_up(bp + 8)))
	(*TIntegrityCk)(unsafe.Pointer(pCheck)).FzPfx = saved_zPfx
	(*TIntegrityCk)(unsafe.Pointer(pCheck)).Fv1 = libc.Uint32FromInt32(saved_v1)
	(*TIntegrityCk)(unsafe.Pointer(pCheck)).Fv2 = saved_v2
	return depth + int32(1)
}

// C documentation
//
//	/*
//	** Erase the given database page and all its children.  Return
//	** the page to the freelist.
//	*/
func _clearDatabasePage(tls *libc.TLS, pBt uintptr, pgno TPgno, freePageFlag int32, pnChange uintptr) (r int32) {
	bp := tls.Alloc(48)
	defer tls.Free(48)
	var hdr, i, v2 int32
	var pCell uintptr
	var _ /* info at bp+16 */ TCellInfo
	var _ /* pPage at bp+0 */ uintptr
	var _ /* rc at bp+8 */ int32
	_, _, _, _ = hdr, i, pCell, v2
	if pgno > _btreePagecount(tls, pBt) {
		return _sqlite3CorruptError(tls, int32(83360))
	}
	**(**int32)(__ccgo_up(bp + 8)) = _getAndInitPage(tls, pBt, pgno, bp, 0)
	if **(**int32)(__ccgo_up(bp + 8)) != 0 {
		return **(**int32)(__ccgo_up(bp + 8))
	}
	if libc.Int32FromUint8((*TBtShared)(unsafe.Pointer(pBt)).FopenFlags)&int32(BTREE_SINGLE) == 0 && _sqlite3PagerPageRefcount(tls, (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FpDbPage) != int32(1)+libc.BoolInt32(pgno == uint32(1)) {
		**(**int32)(__ccgo_up(bp + 8)) = _sqlite3CorruptError(tls, int32(83367))
		goto cleardatabasepage_out
	}
	hdr = libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FhdrOffset)
	i = 0
	for {
		if !(i < libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FnCell)) {
			break
		}
		pCell = (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FaCellIdx + uintptr(int32(2)*i)))))
		if !((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).Fleaf != 0) {
			**(**int32)(__ccgo_up(bp + 8)) = _clearDatabasePage(tls, pBt, _sqlite3Get4byte(tls, pCell), int32(1), pnChange)
			if **(**int32)(__ccgo_up(bp + 8)) != 0 {
				goto cleardatabasepage_out
			}
		}
		(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FxParseCell})))(tls, **(**uintptr)(__ccgo_up(bp)), pCell, bp+16)
		if uint32((**(**TCellInfo)(__ccgo_up(bp + 16))).FnLocal) != (**(**TCellInfo)(__ccgo_up(bp + 16))).FnPayload {
			**(**int32)(__ccgo_up(bp + 8)) = _clearCellOverflow(tls, **(**uintptr)(__ccgo_up(bp)), pCell, bp+16)
		} else {
			**(**int32)(__ccgo_up(bp + 8)) = SQLITE_OK
		}
		if **(**int32)(__ccgo_up(bp + 8)) != 0 {
			goto cleardatabasepage_out
		}
		goto _1
	_1:
		;
		i = i + 1
	}
	if !((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).Fleaf != 0) {
		**(**int32)(__ccgo_up(bp + 8)) = _clearDatabasePage(tls, pBt, _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FaData+uintptr(hdr+int32(8))), int32(1), pnChange)
		if **(**int32)(__ccgo_up(bp + 8)) != 0 {
			goto cleardatabasepage_out
		}
		if (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FintKey != 0 {
			pnChange = uintptr(0)
		}
	}
	if pnChange != 0 {
		**(**Ti64)(__ccgo_up(pnChange)) += libc.Int64FromUint16((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FnCell)
	}
	if freePageFlag != 0 {
		_freePage(tls, **(**uintptr)(__ccgo_up(bp)), bp+8)
	} else {
		v2 = _sqlite3PagerWrite(tls, (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FpDbPage)
		**(**int32)(__ccgo_up(bp + 8)) = v2
		if v2 == 0 {
			_zeroPage(tls, **(**uintptr)(__ccgo_up(bp)), libc.Int32FromUint8(**(**Tu8)(__ccgo_up((*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp)))).FaData + uintptr(hdr))))|int32(PTF_LEAF))
		}
	}
	goto cleardatabasepage_out
cleardatabasepage_out:
	;
	_releasePage(tls, **(**uintptr)(__ccgo_up(bp)))
	return **(**int32)(__ccgo_up(bp + 8))
}

// C documentation
//
//	/*
//	** Create a new collating function for database "db".  The name is zName
//	** and the encoding is enc.
//	*/
func _createCollation(tls *libc.TLS, db uintptr, zName uintptr, enc Tu8, pCtx uintptr, __ccgo_fp_xCompare uintptr, __ccgo_fp_xDel uintptr) (r int32) {
	var aColl, p, pColl uintptr
	var enc2, j int32
	_, _, _, _, _ = aColl, enc2, j, p, pColl
	/* If SQLITE_UTF16 is specified as the encoding type, transform this
	 ** to one of SQLITE_UTF16LE or SQLITE_UTF16BE using the
	 ** SQLITE_UTF16NATIVE macro. SQLITE_UTF16 is not used internally.
	 */
	enc2 = libc.Int32FromUint8(enc)
	if enc2 == int32(SQLITE_UTF16) || enc2 == int32(SQLITE_UTF16_ALIGNED) {
		enc2 = int32(SQLITE_UTF16BE)
	}
	if enc2 < int32(SQLITE_UTF8) || enc2 > int32(SQLITE_UTF16BE) {
		return _sqlite3MisuseError(tls, int32(190075))
	}
	/* Check if this call is removing or replacing an existing collation
	 ** sequence. If so, and there are active VMs, return busy. If there
	 ** are no active VMs, invalidate any pre-compiled statements.
	 */
	pColl = _sqlite3FindCollSeq(tls, db, libc.Uint8FromInt32(enc2), zName, 0)
	if pColl != 0 && (*TCollSeq)(unsafe.Pointer(pColl)).FxCmp != 0 {
		if (*Tsqlite3)(unsafe.Pointer(db)).FnVdbeActive != 0 {
			_sqlite3ErrorWithMsg(tls, db, int32(SQLITE_BUSY), __ccgo_ts+25981, 0)
			return int32(SQLITE_BUSY)
		}
		_sqlite3ExpirePreparedStatements(tls, db, 0)
		/* If collation sequence pColl was created directly by a call to
		 ** sqlite3_create_collation, and not generated by synthCollSeq(),
		 ** then any copies made by synthCollSeq() need to be invalidated.
		 ** Also, collation destructor - CollSeq.xDel() - function may need
		 ** to be called.
		 */
		if libc.Int32FromUint8((*TCollSeq)(unsafe.Pointer(pColl)).Fenc) & ^libc.Int32FromInt32(SQLITE_UTF16_ALIGNED) == enc2 {
			aColl = _sqlite3HashFind(tls, db+648, zName)
			j = 0
			for {
				if !(j < int32(3)) {
					break
				}
				p = aColl + uintptr(j)*40
				if libc.Int32FromUint8((*TCollSeq)(unsafe.Pointer(p)).Fenc) == libc.Int32FromUint8((*TCollSeq)(unsafe.Pointer(pColl)).Fenc) {
					if (*TCollSeq)(unsafe.Pointer(p)).FxDel != 0 {
						(*(*func(*libc.TLS, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TCollSeq)(unsafe.Pointer(p)).FxDel})))(tls, (*TCollSeq)(unsafe.Pointer(p)).FpUser)
					}
					(*TCollSeq)(unsafe.Pointer(p)).FxCmp = uintptr(0)
				}
				goto _1
			_1:
				;
				j = j + 1
			}
		}
	}
	pColl = _sqlite3FindCollSeq(tls, db, libc.Uint8FromInt32(enc2), zName, int32(1))
	if pColl == uintptr(0) {
		return int32(SQLITE_NOMEM)
	}
	(*TCollSeq)(unsafe.Pointer(pColl)).FxCmp = __ccgo_fp_xCompare
	(*TCollSeq)(unsafe.Pointer(pColl)).FpUser = pCtx
	(*TCollSeq)(unsafe.Pointer(pColl)).FxDel = __ccgo_fp_xDel
	(*TCollSeq)(unsafe.Pointer(pColl)).Fenc = libc.Uint8FromInt32(enc2 | libc.Int32FromUint8(enc)&libc.Int32FromInt32(SQLITE_UTF16_ALIGNED))
	_sqlite3Error(tls, db, SQLITE_OK)
	return SQLITE_OK
}

// C documentation
//
//	/*
//	** Compare the "idx"-th cell on the page pPage against the key
//	** pointing to by pIdxKey using xRecordCompare.  Return negative or
//	** zero if the cell is less than or equal pIdxKey.  Return positive
//	** if unknown.
//	**
//	**    Return value negative:     Cell at pCur[idx] less than pIdxKey
//	**
//	**    Return value is zero:      Cell at pCur[idx] equals pIdxKey
//	**
//	**    Return value positive:     Nothing is known about the relationship
//	**                               of the cell at pCur[idx] and pIdxKey.
//	**
//	** This routine is part of an optimization.  It is always safe to return
//	** a positive value as that will cause the optimization to be skipped.
//	*/
func _indexCellCompare(tls *libc.TLS, pPage uintptr, idx int32, pIdxKey uintptr, __ccgo_fp_xRecordCompare TRecordCompare) (r int32) {
	var c, nCell, v1 int32
	var pCell uintptr
	var v2 bool
	_, _, _, _, _ = c, nCell, pCell, v1, v2 /* Size of the pCell cell in bytes */
	pCell = (*TMemPage)(unsafe.Pointer(pPage)).FaDataOfst + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*idx)))))
	nCell = libc.Int32FromUint8(**(**Tu8)(__ccgo_up(pCell)))
	if nCell <= libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pPage)).Fmax1bytePayload) {
		/* This branch runs if the record-size field of the cell is a
		 ** single byte varint and the record fits entirely on the main
		 ** b-tree page.  */
		c = (*(*func(*libc.TLS, int32, uintptr, uintptr) int32)(unsafe.Pointer(&struct{ uintptr }{__ccgo_fp_xRecordCompare})))(tls, nCell, pCell+1, pIdxKey)
	} else {
		if v2 = !(libc.Int32FromUint8(**(**Tu8)(__ccgo_up(pCell + 1)))&libc.Int32FromInt32(0x80) != 0); v2 {
			v1 = nCell&libc.Int32FromInt32(0x7f)<<libc.Int32FromInt32(7) + libc.Int32FromUint8(**(**Tu8)(__ccgo_up(pCell + 1)))
			nCell = v1
		}
		if v2 && v1 <= libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaxLocal) {
			/* The record-size field is a 2 byte varint and the record
			 ** fits entirely on the main b-tree page.  */
			c = (*(*func(*libc.TLS, int32, uintptr, uintptr) int32)(unsafe.Pointer(&struct{ uintptr }{__ccgo_fp_xRecordCompare})))(tls, nCell, pCell+2, pIdxKey)
		} else {
			/* If the record extends into overflow pages, do not attempt
			 ** the optimization. */
			c = int32(99)
		}
	}
	return c
}

// C documentation
//
//	/*
//	** Somewhere on pPage is a pointer to page iFrom.  Modify this pointer so
//	** that it points to iTo. Parameter eType describes the type of pointer to
//	** be modified, as  follows:
//	**
//	** PTRMAP_BTREE:     pPage is a btree-page. The pointer points at a child
//	**                   page of pPage.
//	**
//	** PTRMAP_OVERFLOW1: pPage is a btree-page. The pointer points at an overflow
//	**                   page pointed to by one of the cells on pPage.
//	**
//	** PTRMAP_OVERFLOW2: pPage is an overflow-page. The pointer points at the next
//	**                   overflow page in the list.
//	*/
func _modifyPagePointer(tls *libc.TLS, pPage uintptr, iFrom TPgno, iTo TPgno, eType Tu8) (r int32) {
	bp := tls.Alloc(32)
	defer tls.Free(32)
	var i, nCell, rc, v1 int32
	var pCell uintptr
	var _ /* info at bp+0 */ TCellInfo
	_, _, _, _, _ = i, nCell, pCell, rc, v1
	if libc.Int32FromUint8(eType) == int32(PTRMAP_OVERFLOW2) {
		/* The pointer is always the first 4 bytes of the page in this case.  */
		if _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData) != iFrom {
			return _sqlite3CorruptError(tls, int32(77023))
		}
		_sqlite3Put4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData, iTo)
	} else {
		if (*TMemPage)(unsafe.Pointer(pPage)).FisInit != 0 {
			v1 = SQLITE_OK
		} else {
			v1 = _btreeInitPage(tls, pPage)
		}
		rc = v1
		if rc != 0 {
			return rc
		}
		nCell = libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell)
		i = 0
		for {
			if !(i < nCell) {
				break
			}
			pCell = (*TMemPage)(unsafe.Pointer(pPage)).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*i)))))
			if libc.Int32FromUint8(eType) == int32(PTRMAP_OVERFLOW1) {
				(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxParseCell})))(tls, pPage, pCell, bp)
				if uint32((**(**TCellInfo)(__ccgo_up(bp))).FnLocal) < (**(**TCellInfo)(__ccgo_up(bp))).FnPayload {
					if pCell+uintptr((**(**TCellInfo)(__ccgo_up(bp))).FnSize) > (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr((*TBtShared)(unsafe.Pointer((*TMemPage)(unsafe.Pointer(pPage)).FpBt)).FusableSize) {
						return _sqlite3CorruptError(tls, int32(77042))
					}
					if iFrom == _sqlite3Get4byte(tls, pCell+uintptr((**(**TCellInfo)(__ccgo_up(bp))).FnSize)-uintptr(4)) {
						_sqlite3Put4byte(tls, pCell+uintptr((**(**TCellInfo)(__ccgo_up(bp))).FnSize)-uintptr(4), iTo)
						break
					}
				}
			} else {
				if pCell+uintptr(4) > (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr((*TBtShared)(unsafe.Pointer((*TMemPage)(unsafe.Pointer(pPage)).FpBt)).FusableSize) {
					return _sqlite3CorruptError(tls, int32(77051))
				}
				if _sqlite3Get4byte(tls, pCell) == iFrom {
					_sqlite3Put4byte(tls, pCell, iTo)
					break
				}
			}
			goto _2
		_2:
			;
			i = i + 1
		}
		if i == nCell {
			if libc.Int32FromUint8(eType) != int32(PTRMAP_BTREE) || _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pPage)).FhdrOffset)+int32(8))) != iFrom {
				return _sqlite3CorruptError(tls, int32(77063))
			}
			_sqlite3Put4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pPage)).FhdrOffset)+int32(8)), iTo)
		}
	}
	return SQLITE_OK
}

// C documentation
//
//	/*
//	** Move the cursor down to the left-most leaf entry beneath the
//	** entry to which it is currently pointing.
//	**
//	** The left-most leaf is the one with the smallest key - the first
//	** in ascending order.
//	*/
func _moveToLeftmost(tls *libc.TLS, pCur uintptr) (r int32) {
	var pPage, v1 uintptr
	var pgno TPgno
	var rc int32
	var v2 bool
	_, _, _, _, _ = pPage, pgno, rc, v1, v2
	rc = SQLITE_OK
	for {
		if v2 = rc == SQLITE_OK; v2 {
			v1 = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
			pPage = v1
		}
		if !(v2 && !((*TMemPage)(unsafe.Pointer(v1)).Fleaf != 0)) {
			break
		}
		pgno = _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix)))))))
		rc = _moveToChild(tls, pCur, pgno)
	}
	return rc
}

func _readCoord(tls *libc.TLS, p uintptr, pCoord uintptr) {
	*(*Tu32)(unsafe.Pointer(pCoord)) = **(**Tu32)(__ccgo_up(p))
}

func _readInt64(tls *libc.TLS, p uintptr) (r Ti64) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var _ /* x at bp+0 */ Ti64
	libc.Xmemcpy(tls, bp, p, uint64(8))
	return **(**Ti64)(__ccgo_up(bp))
}

// C documentation
//
//	/*
//	** Check the leaf RTree cell given by pCellData against constraint p.
//	** If this constraint is not satisfied, set *peWithin to NOT_WITHIN.
//	** If the constraint is satisfied, leave *peWithin unchanged.
//	**
//	** The constraint is of the form:  xN op $val
//	**
//	** The op is given by p->op.  The xN is p->iCoord-th coordinate in
//	** pCellData.  $val is given by p->u.rValue.
//	*/
func _rtreeLeafConstraint(tls *libc.TLS, p uintptr, eInt int32, pCellData uintptr, peWithin uintptr) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var xN TRtreeDValue
	var v1 Tsqlite3_rtree_dbl
	var _ /* c at bp+0 */ TRtreeCoord
	_, _ = xN, v1 /* Coordinate value converted to a double */
	pCellData = pCellData + uintptr(int32(8)+(*TRtreeConstraint)(unsafe.Pointer(p)).FiCoord*int32(4))
	/* Coordinate decoded */
	libc.Xmemcpy(tls, bp, pCellData, uint64(4))
	if eInt != 0 {
		v1 = float64(*(*int32)(unsafe.Pointer(bp)))
	} else {
		v1 = float64(*(*TRtreeValue)(unsafe.Pointer(bp)))
	}
	xN = v1
	switch (*TRtreeConstraint)(unsafe.Pointer(p)).Fop {
	case int32(RTREE_TRUE):
		return /* Always satisfied */
	case int32(RTREE_FALSE):
	case int32(RTREE_LE):
		if xN <= *(*TRtreeDValue)(unsafe.Pointer(p + 8)) {
			return
		}
	case int32(RTREE_LT):
		if xN < *(*TRtreeDValue)(unsafe.Pointer(p + 8)) {
			return
		}
	case int32(RTREE_GE):
		if xN >= *(*TRtreeDValue)(unsafe.Pointer(p + 8)) {
			return
		}
	case int32(RTREE_GT):
		if xN > *(*TRtreeDValue)(unsafe.Pointer(p + 8)) {
			return
		}
	default:
		if xN == *(*TRtreeDValue)(unsafe.Pointer(p + 8)) {
			return
		}
		break
	}
	**(**int32)(__ccgo_up(peWithin)) = NOT_WITHIN
}

// C documentation
//
//	/*
//	** Check the internal RTree node given by pCellData against constraint p.
//	** If this constraint cannot be satisfied by any child within the node,
//	** set *peWithin to NOT_WITHIN.
//	*/
func _rtreeNonleafConstraint(tls *libc.TLS, p uintptr, eInt int32, pCellData uintptr, peWithin uintptr) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var val, v1 Tsqlite3_rtree_dbl
	var _ /* c at bp+0 */ TRtreeCoord
	var _ /* c at bp+12 */ TRtreeCoord
	var _ /* c at bp+4 */ TRtreeCoord
	var _ /* c at bp+8 */ TRtreeCoord
	_, _ = val, v1 /* Coordinate value convert to a double */
	/* p->iCoord might point to either a lower or upper bound coordinate
	 ** in a coordinate pair.  But make pCellData point to the lower bound.
	 */
	pCellData = pCellData + uintptr(int32(8)+int32(4)*((*TRtreeConstraint)(unsafe.Pointer(p)).FiCoord&int32(0xfe)))
	switch (*TRtreeConstraint)(unsafe.Pointer(p)).Fop {
	case int32(RTREE_TRUE):
		return /* Always satisfied */
	case int32(RTREE_FALSE):
	case int32(RTREE_EQ):
		/* Coordinate decoded */ libc.Xmemcpy(tls, bp, pCellData, uint64(4))
		if eInt != 0 {
			v1 = float64(*(*int32)(unsafe.Pointer(bp)))
		} else {
			v1 = float64(*(*TRtreeValue)(unsafe.Pointer(bp)))
		}
		val = v1
		/* val now holds the lower bound of the coordinate pair */
		if *(*TRtreeDValue)(unsafe.Pointer(p + 8)) >= val {
			pCellData = pCellData + uintptr(4)
			/* Coordinate decoded */ libc.Xmemcpy(tls, bp+4, pCellData, uint64(4))
			if eInt != 0 {
				v1 = float64(*(*int32)(unsafe.Pointer(bp + 4)))
			} else {
				v1 = float64(*(*TRtreeValue)(unsafe.Pointer(bp + 4)))
			}
			val = v1
			/* val now holds the upper bound of the coordinate pair */
			if *(*TRtreeDValue)(unsafe.Pointer(p + 8)) <= val {
				return
			}
		}
	case int32(RTREE_LE):
		fallthrough
	case int32(RTREE_LT):
		/* Coordinate decoded */ libc.Xmemcpy(tls, bp+8, pCellData, uint64(4))
		if eInt != 0 {
			v1 = float64(*(*int32)(unsafe.Pointer(bp + 8)))
		} else {
			v1 = float64(*(*TRtreeValue)(unsafe.Pointer(bp + 8)))
		}
		val = v1
		/* val now holds the lower bound of the coordinate pair */
		if *(*TRtreeDValue)(unsafe.Pointer(p + 8)) >= val {
			return
		}
	default:
		pCellData = pCellData + uintptr(4)
		/* Coordinate decoded */ libc.Xmemcpy(tls, bp+12, pCellData, uint64(4))
		if eInt != 0 {
			v1 = float64(*(*int32)(unsafe.Pointer(bp + 12)))
		} else {
			v1 = float64(*(*TRtreeValue)(unsafe.Pointer(bp + 12)))
		}
		val = v1
		/* val now holds the upper bound of the coordinate pair */
		if *(*TRtreeDValue)(unsafe.Pointer(p + 8)) <= val {
			return
		}
		break
	}
	**(**int32)(__ccgo_up(peWithin)) = NOT_WITHIN
}

// C documentation
//
//	/*
//	** Set the pointer-map entries for all children of page pPage. Also, if
//	** pPage contains cells that point to overflow pages, set the pointer
//	** map entries for the overflow pages as well.
//	*/
func _setChildPtrmaps(tls *libc.TLS, pPage uintptr) (r int32) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var childPgno, childPgno1, pgno TPgno
	var i, nCell, v1 int32
	var pBt, pCell uintptr
	var _ /* rc at bp+0 */ int32
	_, _, _, _, _, _, _, _ = childPgno, childPgno1, i, nCell, pBt, pCell, pgno, v1 /* Return code */
	pBt = (*TMemPage)(unsafe.Pointer(pPage)).FpBt
	pgno = (*TMemPage)(unsafe.Pointer(pPage)).Fpgno
	if (*TMemPage)(unsafe.Pointer(pPage)).FisInit != 0 {
		v1 = SQLITE_OK
	} else {
		v1 = _btreeInitPage(tls, pPage)
	}
	**(**int32)(__ccgo_up(bp)) = v1
	if **(**int32)(__ccgo_up(bp)) != SQLITE_OK {
		return **(**int32)(__ccgo_up(bp))
	}
	nCell = libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell)
	i = 0
	for {
		if !(i < nCell) {
			break
		}
		pCell = (*TMemPage)(unsafe.Pointer(pPage)).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*i)))))
		_ptrmapPutOvflPtr(tls, pPage, pPage, pCell, bp)
		if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
			childPgno = _sqlite3Get4byte(tls, pCell)
			_ptrmapPut(tls, pBt, childPgno, uint8(PTRMAP_BTREE), pgno, bp)
		}
		goto _2
	_2:
		;
		i = i + 1
	}
	if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
		childPgno1 = _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pPage)).FhdrOffset)+int32(8)))
		_ptrmapPut(tls, pBt, childPgno1, uint8(PTRMAP_BTREE), pgno, bp)
	}
	return **(**int32)(__ccgo_up(bp))
}

// C documentation
//
//	/*
//	** The first argument, pCur, is a cursor opened on some b-tree. Count the
//	** number of entries in the b-tree and write the result to *pnEntry.
//	**
//	** SQLITE_OK is returned if the operation is successfully executed.
//	** Otherwise, if an error is encountered (i.e. an IO error or database
//	** corruption) an SQLite error code is returned.
//	*/
func _sqlite3BtreeCount(tls *libc.TLS, db uintptr, pCur uintptr, pnEntry uintptr) (r int32) {
	var iIdx, rc int32
	var nEntry Ti64
	var pPage uintptr
	_, _, _, _ = iIdx, nEntry, pPage, rc
	nEntry = 0 /* Return code */
	rc = _moveToRoot(tls, pCur)
	if rc == int32(SQLITE_EMPTY) {
		**(**Ti64)(__ccgo_up(pnEntry)) = 0
		return SQLITE_OK
	}
	/* Unless an error occurs, the following loop runs one iteration for each
	 ** page in the B-Tree structure (not including overflow pages).
	 */
	for rc == SQLITE_OK && !(libc.AtomicLoadNInt32(db+432, libc.Int32FromInt32(__ATOMIC_RELAXED)) != 0) { /* Current page of the b-tree */
		/* If this is a leaf page or the tree is not an int-key tree, then
		 ** this page contains countable entries. Increment the entry counter
		 ** accordingly.
		 */
		pPage = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
		if (*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0 || !((*TMemPage)(unsafe.Pointer(pPage)).FintKey != 0) {
			nEntry = nEntry + libc.Int64FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell)
		}
		/* pPage is a leaf node. This loop navigates the cursor so that it
		 ** points to the first interior cell that it points to the parent of
		 ** the next page in the tree that has not yet been visited. The
		 ** pCur->aiIdx[pCur->iPage] value is set to the index of the parent cell
		 ** of the page, or to the number of cells in the page if the next page
		 ** to visit is the right-child of its parent.
		 **
		 ** If all pages in the tree have been visited, return SQLITE_OK to the
		 ** caller.
		 */
		if (*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0 {
			for cond := true; cond; cond = libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix) >= libc.Int32FromUint16((*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).FnCell) {
				if int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage) == 0 {
					/* All pages of the b-tree have been visited. Return successfully. */
					**(**Ti64)(__ccgo_up(pnEntry)) = nEntry
					return _moveToRoot(tls, pCur)
				}
				_moveToParent(tls, pCur)
			}
			(*TBtCursor)(unsafe.Pointer(pCur)).Fix = (*TBtCursor)(unsafe.Pointer(pCur)).Fix + 1
			pPage = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
		}
		/* Descend to the child node of the cell that the cursor currently
		 ** points at. This is the right-child if (iIdx==pPage->nCell).
		 */
		iIdx = libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix)
		if iIdx == libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) {
			rc = _moveToChild(tls, pCur, _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pPage)).FhdrOffset)+int32(8))))
		} else {
			rc = _moveToChild(tls, pCur, _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*iIdx)))))))
		}
	}
	/* An error has occurred. Return an error code. */
	return rc
}

// C documentation
//
//	/*
//	** Delete the entry that the cursor is pointing to.
//	**
//	** If the BTREE_SAVEPOSITION bit of the flags parameter is zero, then
//	** the cursor is left pointing at an arbitrary location after the delete.
//	** But if that bit is set, then the cursor is left in a state such that
//	** the next call to BtreeNext() or BtreePrev() moves it to the same row
//	** as it would have been on if the call to BtreeDelete() had been omitted.
//	**
//	** The BTREE_AUXDELETE bit of flags indicates that is one of several deletes
//	** associated with a single table entry and its indexes.  Only one of those
//	** deletes is considered the "primary" delete.  The primary delete occurs
//	** on a cursor that is not a BTREE_FORDELETE cursor.  All but one delete
//	** operation on non-FORDELETE cursors is tagged with the AUXDELETE flag.
//	** The BTREE_AUXDELETE bit is a hint that is not used by this implementation,
//	** but which might be used by alternative storage engines.
//	*/
func _sqlite3BtreeDelete(tls *libc.TLS, pCur uintptr, flags Tu8) (r int32) {
	bp := tls.Alloc(32)
	defer tls.Free(32)
	var bPreserve Tu8
	var iCellDepth, iCellIdx, nCell int32
	var n TPgno
	var p, pBt, pCell, pLeaf, pPage, pTmp, v2 uintptr
	var v1 Ti8
	var _ /* info at bp+8 */ TCellInfo
	var _ /* rc at bp+0 */ int32
	_, _, _, _, _, _, _, _, _, _, _, _, _ = bPreserve, iCellDepth, iCellIdx, n, nCell, p, pBt, pCell, pLeaf, pPage, pTmp, v1, v2
	p = (*TBtCursor)(unsafe.Pointer(pCur)).FpBtree
	pBt = (*TBtree)(unsafe.Pointer(p)).FpBt /* Keep cursor valid.  2 for CURSOR_SKIPNEXT */
	if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) != CURSOR_VALID {
		if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) >= int32(CURSOR_REQUIRESEEK) {
			**(**int32)(__ccgo_up(bp)) = _btreeRestoreCursorPosition(tls, pCur)
			if **(**int32)(__ccgo_up(bp)) != 0 || libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) != CURSOR_VALID {
				return **(**int32)(__ccgo_up(bp))
			}
		} else {
			return _sqlite3CorruptError(tls, int32(82999))
		}
	}
	iCellDepth = int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage)
	iCellIdx = libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix)
	pPage = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
	if libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) <= iCellIdx {
		return _sqlite3CorruptError(tls, int32(83008))
	}
	pCell = (*TMemPage)(unsafe.Pointer(pPage)).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*iCellIdx)))))
	if (*TMemPage)(unsafe.Pointer(pPage)).FnFree < 0 && _btreeComputeFreeSpace(tls, pPage) != 0 {
		return _sqlite3CorruptError(tls, int32(83012))
	}
	if pCell < (*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx+uintptr((*TMemPage)(unsafe.Pointer(pPage)).FnCell) {
		return _sqlite3CorruptError(tls, int32(83015))
	}
	/* If the BTREE_SAVEPOSITION bit is on, then the cursor position must
	 ** be preserved following this delete operation. If the current delete
	 ** will cause a b-tree rebalance, then this is done by saving the cursor
	 ** key and leaving the cursor in CURSOR_REQUIRESEEK state before
	 ** returning.
	 **
	 ** If the current delete will not cause a rebalance, then the cursor
	 ** will be left in CURSOR_SKIPNEXT state pointing to the entry immediately
	 ** before or after the deleted entry.
	 **
	 ** The bPreserve value records which path is required:
	 **
	 **    bPreserve==0         Not necessary to save the cursor position
	 **    bPreserve==1         Use CURSOR_REQUIRESEEK to save the cursor position
	 **    bPreserve==2         Cursor won't move.  Set CURSOR_SKIPNEXT.
	 */
	bPreserve = libc.BoolUint8(libc.Int32FromUint8(flags)&int32(BTREE_SAVEPOSITION) != 0)
	if bPreserve != 0 {
		if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) || (*TMemPage)(unsafe.Pointer(pPage)).FnFree+libc.Int32FromUint16((*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxCellSize})))(tls, pPage, pCell))+int32(2) > libc.Int32FromUint32((*TBtShared)(unsafe.Pointer(pBt)).FusableSize*libc.Uint32FromInt32(2)/libc.Uint32FromInt32(3)) || libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) == int32(1) {
			/* A b-tree rebalance will be required after deleting this entry.
			 ** Save the cursor key.  */
			**(**int32)(__ccgo_up(bp)) = _saveCursorKey(tls, pCur)
			if **(**int32)(__ccgo_up(bp)) != 0 {
				return **(**int32)(__ccgo_up(bp))
			}
		} else {
			bPreserve = uint8(2)
		}
	}
	/* If the page containing the entry to delete is not a leaf page, move
	 ** the cursor to the largest entry in the tree that is smaller than
	 ** the entry being deleted. This cell will replace the cell being deleted
	 ** from the internal node. The 'previous' entry is used for this instead
	 ** of the 'next' entry, as the previous entry is always a part of the
	 ** sub-tree headed by the child page of the cell being deleted. This makes
	 ** balancing the tree following the delete operation easier.  */
	if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
		**(**int32)(__ccgo_up(bp)) = _sqlite3BtreePrevious(tls, pCur, 0)
		if **(**int32)(__ccgo_up(bp)) != 0 {
			return **(**int32)(__ccgo_up(bp))
		}
	}
	/* Save the positions of any other cursors open on this table before
	 ** making any modifications.  */
	if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FcurFlags)&int32(BTCF_Multiple) != 0 {
		**(**int32)(__ccgo_up(bp)) = _saveAllCursors(tls, pBt, (*TBtCursor)(unsafe.Pointer(pCur)).FpgnoRoot, pCur)
		if **(**int32)(__ccgo_up(bp)) != 0 {
			return **(**int32)(__ccgo_up(bp))
		}
	}
	/* If this is a delete operation to remove a row from a table b-tree,
	 ** invalidate any incrblob cursors open on the row being deleted.  */
	if (*TBtCursor)(unsafe.Pointer(pCur)).FpKeyInfo == uintptr(0) && (*TBtree)(unsafe.Pointer(p)).FhasIncrblobCur != 0 {
		_invalidateIncrblobCursors(tls, p, (*TBtCursor)(unsafe.Pointer(pCur)).FpgnoRoot, (*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey, 0)
	}
	/* Make the page containing the entry to be deleted writable. Then free any
	 ** overflow pages associated with the entry and finally remove the cell
	 ** itself from within the page.  */
	**(**int32)(__ccgo_up(bp)) = _sqlite3PagerWrite(tls, (*TMemPage)(unsafe.Pointer(pPage)).FpDbPage)
	if **(**int32)(__ccgo_up(bp)) != 0 {
		return **(**int32)(__ccgo_up(bp))
	}
	(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxParseCell})))(tls, pPage, pCell, bp+8)
	if uint32((**(**TCellInfo)(__ccgo_up(bp + 8))).FnLocal) != (**(**TCellInfo)(__ccgo_up(bp + 8))).FnPayload {
		**(**int32)(__ccgo_up(bp)) = _clearCellOverflow(tls, pPage, pCell, bp+8)
	} else {
		**(**int32)(__ccgo_up(bp)) = SQLITE_OK
	}
	_dropCell(tls, pPage, iCellIdx, libc.Int32FromUint16((**(**TCellInfo)(__ccgo_up(bp + 8))).FnSize), bp)
	if **(**int32)(__ccgo_up(bp)) != 0 {
		return **(**int32)(__ccgo_up(bp))
	}
	/* If the cell deleted was not located on a leaf page, then the cursor
	 ** is currently pointing to the largest entry in the sub-tree headed
	 ** by the child-page of the cell that was just deleted from an internal
	 ** node. The cell from the leaf node needs to be moved to the internal
	 ** node to replace the deleted cell.  */
	if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
		pLeaf = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
		if (*TMemPage)(unsafe.Pointer(pLeaf)).FnFree < 0 {
			**(**int32)(__ccgo_up(bp)) = _btreeComputeFreeSpace(tls, pLeaf)
			if **(**int32)(__ccgo_up(bp)) != 0 {
				return **(**int32)(__ccgo_up(bp))
			}
		}
		if iCellDepth < int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage)-int32(1) {
			n = (*TMemPage)(unsafe.Pointer(**(**uintptr)(__ccgo_up(pCur + 144 + uintptr(iCellDepth+int32(1))*8)))).Fpgno
		} else {
			n = (*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).Fpgno
		}
		pCell = (*TMemPage)(unsafe.Pointer(pLeaf)).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pLeaf)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pLeaf)).FaCellIdx + uintptr(int32(2)*(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pLeaf)).FnCell)-int32(1)))))))
		if pCell < (*TMemPage)(unsafe.Pointer(pLeaf)).FaData+4 {
			return _sqlite3CorruptError(tls, int32(83106))
		}
		nCell = libc.Int32FromUint16((*(*func(*libc.TLS, uintptr, uintptr) Tu16)(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pLeaf)).FxCellSize})))(tls, pLeaf, pCell))
		pTmp = (*TBtShared)(unsafe.Pointer(pBt)).FpTmpSpace
		**(**int32)(__ccgo_up(bp)) = _sqlite3PagerWrite(tls, (*TMemPage)(unsafe.Pointer(pLeaf)).FpDbPage)
		if **(**int32)(__ccgo_up(bp)) == SQLITE_OK {
			**(**int32)(__ccgo_up(bp)) = _insertCell(tls, pPage, iCellIdx, pCell-uintptr(4), nCell+int32(4), pTmp, n)
		}
		_dropCell(tls, pLeaf, libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pLeaf)).FnCell)-int32(1), nCell, bp)
		if **(**int32)(__ccgo_up(bp)) != 0 {
			return **(**int32)(__ccgo_up(bp))
		}
	}
	/* Balance the tree. If the entry deleted was located on a leaf page,
	 ** then the cursor still points to that page. In this case the first
	 ** call to balance() repairs the tree, and the if(...) condition is
	 ** never true.
	 **
	 ** Otherwise, if the entry deleted was on an internal node page, then
	 ** pCur is pointing to the leaf page from which a cell was removed to
	 ** replace the cell deleted from the internal node. This is slightly
	 ** tricky as the leaf node may be underfull, and the internal node may
	 ** be either under or overfull. In this case run the balancing algorithm
	 ** on the leaf node first. If the balance proceeds far enough up the
	 ** tree that we can be sure that any problem in the internal node has
	 ** been corrected, so be it. Otherwise, after balancing the leaf node,
	 ** walk the cursor up the tree to the internal node and balance it as
	 ** well.  */
	if (*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).FnFree*int32(3) <= libc.Int32FromUint32((*TBtShared)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpBt)).FusableSize)*int32(2) {
		/* Optimization: If the free space is less than 2/3rds of the page,
		 ** then balance() will always be a no-op.  No need to invoke it. */
		**(**int32)(__ccgo_up(bp)) = SQLITE_OK
	} else {
		**(**int32)(__ccgo_up(bp)) = _balance(tls, pCur)
	}
	if **(**int32)(__ccgo_up(bp)) == SQLITE_OK && int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage) > iCellDepth {
		_releasePageNotNull(tls, (*TBtCursor)(unsafe.Pointer(pCur)).FpPage)
		(*TBtCursor)(unsafe.Pointer(pCur)).FiPage = (*TBtCursor)(unsafe.Pointer(pCur)).FiPage - 1
		for int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage) > iCellDepth {
			v2 = pCur + 84
			v1 = *(*Ti8)(unsafe.Pointer(v2))
			*(*Ti8)(unsafe.Pointer(v2)) = *(*Ti8)(unsafe.Pointer(v2)) - 1
			_releasePage(tls, **(**uintptr)(__ccgo_up(pCur + 144 + uintptr(v1)*8)))
		}
		(*TBtCursor)(unsafe.Pointer(pCur)).FpPage = **(**uintptr)(__ccgo_up(pCur + 144 + uintptr((*TBtCursor)(unsafe.Pointer(pCur)).FiPage)*8))
		**(**int32)(__ccgo_up(bp)) = _balance(tls, pCur)
	}
	if **(**int32)(__ccgo_up(bp)) == SQLITE_OK {
		if libc.Int32FromUint8(bPreserve) > int32(1) {
			(*TBtCursor)(unsafe.Pointer(pCur)).FeState = uint8(CURSOR_SKIPNEXT)
			if iCellIdx >= libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) {
				(*TBtCursor)(unsafe.Pointer(pCur)).FskipNext = -int32(1)
				(*TBtCursor)(unsafe.Pointer(pCur)).Fix = libc.Uint16FromInt32(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) - int32(1))
			} else {
				(*TBtCursor)(unsafe.Pointer(pCur)).FskipNext = int32(1)
			}
		} else {
			**(**int32)(__ccgo_up(bp)) = _moveToRoot(tls, pCur)
			if bPreserve != 0 {
				_btreeReleaseAllCursorPages(tls, pCur)
				(*TBtCursor)(unsafe.Pointer(pCur)).FeState = uint8(CURSOR_REQUIRESEEK)
			}
			if **(**int32)(__ccgo_up(bp)) == int32(SQLITE_EMPTY) {
				**(**int32)(__ccgo_up(bp)) = SQLITE_OK
			}
		}
	}
	return **(**int32)(__ccgo_up(bp))
}

// C documentation
//
//	/* Move the cursor so that it points to an entry in an index table
//	** near the key pIdxKey.   Return a success code.
//	**
//	** If an exact match is not found, then the cursor is always
//	** left pointing at a leaf page which would hold the entry if it
//	** were present.  The cursor might point to an entry that comes
//	** before or after the key.
//	**
//	** An integer is written into *pRes which is the result of
//	** comparing the key with the entry to which the cursor is
//	** pointing.  The meaning of the integer written into
//	** *pRes is as follows:
//	**
//	**     *pRes<0      The cursor is left pointing at an entry that
//	**                  is smaller than pIdxKey or if the table is empty
//	**                  and the cursor is therefore left point to nothing.
//	**
//	**     *pRes==0     The cursor is left pointing at an entry that
//	**                  exactly matches pIdxKey.
//	**
//	**     *pRes>0      The cursor is left pointing at an entry that
//	**                  is larger than pIdxKey.
//	**
//	** The pIdxKey->eqSeen field is set to 1 if there
//	** exists an entry in the table that exactly matches pIdxKey.
//	*/
func _sqlite3BtreeIndexMoveto(tls *libc.TLS, pCur uintptr, pIdxKey uintptr, pRes uintptr) (r int32) {
	var c, c1, idx, lwr, nCell, nOverrun, rc, upr, v1 int32
	var chldPg TPgno
	var pCell, pCellBody, pCellKey, pPage, v3 uintptr
	var xRecordCompare TRecordCompare
	var v10 Ti8
	var v2 bool
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _ = c, c1, chldPg, idx, lwr, nCell, nOverrun, pCell, pCellBody, pCellKey, pPage, rc, upr, xRecordCompare, v1, v10, v2, v3
	xRecordCompare = _sqlite3VdbeFindCompare(tls, pIdxKey)
	(*TUnpackedRecord)(unsafe.Pointer(pIdxKey)).FerrCode = uint8(0)
	/* Check to see if we can skip a lot of work.  Two cases:
	 **
	 **    (1) If the cursor is already pointing to the very last cell
	 **        in the table and the pIdxKey search key is greater than or
	 **        equal to that last cell, then no movement is required.
	 **
	 **    (2) If the cursor is on the last page of the table and the first
	 **        cell on that last page is less than or equal to the pIdxKey
	 **        search key, then we can start the search on the current page
	 **        without needing to go back to root.
	 */
	if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) == CURSOR_VALID && (*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).Fleaf != 0 && _cursorOnLastPage(tls, pCur) != 0 {
		if v2 = libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix) == libc.Int32FromUint16((*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).FnCell)-int32(1); v2 {
			v1 = _indexCellCompare(tls, (*TBtCursor)(unsafe.Pointer(pCur)).FpPage, libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix), pIdxKey, xRecordCompare)
			c = v1
		}
		if v2 && v1 <= 0 && libc.Int32FromUint8((*TUnpackedRecord)(unsafe.Pointer(pIdxKey)).FerrCode) == SQLITE_OK {
			**(**int32)(__ccgo_up(pRes)) = c
			return SQLITE_OK /* Cursor already pointing at the correct spot */
		}
		if int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage) > 0 && _indexCellCompare(tls, (*TBtCursor)(unsafe.Pointer(pCur)).FpPage, 0, pIdxKey, xRecordCompare) <= 0 && libc.Int32FromUint8((*TUnpackedRecord)(unsafe.Pointer(pIdxKey)).FerrCode) == SQLITE_OK {
			v3 = pCur + 1
			*(*Tu8)(unsafe.Pointer(v3)) = Tu8(int32(*(*Tu8)(unsafe.Pointer(v3))) & ^(libc.Int32FromInt32(BTCF_ValidOvfl) | libc.Int32FromInt32(BTCF_AtLast)))
			if !((*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).FisInit != 0) {
				return _sqlite3CorruptError(tls, int32(79227))
			}
			goto bypass_moveto_root /* Start search on the current page */
		}
		(*TUnpackedRecord)(unsafe.Pointer(pIdxKey)).FerrCode = uint8(SQLITE_OK)
	}
	rc = _moveToRoot(tls, pCur)
	if rc != 0 {
		if rc == int32(SQLITE_EMPTY) {
			**(**int32)(__ccgo_up(pRes)) = -int32(1)
			return SQLITE_OK
		}
		return rc
	}
	goto bypass_moveto_root
bypass_moveto_root:
	;
	for {
		pPage = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage /* Pointer to current cell in pPage */
		/* pPage->nCell must be greater than zero. If this is the root-page
		 ** the cursor would have been INVALID above and this for(;;) loop
		 ** not run. If this is not the root-page, then the moveToChild() routine
		 ** would have already detected db corruption. Similarly, pPage must
		 ** be the right kind (index or table) of b-tree page. Otherwise
		 ** a moveToChild() or moveToRoot() call would have detected corruption.  */
		lwr = 0
		upr = libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) - int32(1)
		idx = upr >> int32(1) /* idx = (lwr+upr)/2; */
		for {                 /* Size of the pCell cell in bytes */
			pCell = (*TMemPage)(unsafe.Pointer(pPage)).FaDataOfst + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*idx)))))
			/* The maximum supported page-size is 65536 bytes. This means that
			 ** the maximum number of record bytes stored on an index B-Tree
			 ** page is less than 16384 bytes and may be stored as a 2-byte
			 ** varint. This information is used to attempt to avoid parsing
			 ** the entire cell by checking for the cases where the record is
			 ** stored entirely within the b-tree page by inspecting the first
			 ** 2 bytes of the cell.
			 */
			nCell = libc.Int32FromUint8(**(**Tu8)(__ccgo_up(pCell)))
			if nCell <= libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pPage)).Fmax1bytePayload) {
				/* This branch runs if the record-size field of the cell is a
				 ** single byte varint and the record fits entirely on the main
				 ** b-tree page.  */
				c1 = (*(*func(*libc.TLS, int32, uintptr, uintptr) int32)(unsafe.Pointer(&struct{ uintptr }{xRecordCompare})))(tls, nCell, pCell+1, pIdxKey)
			} else {
				if v2 = !(libc.Int32FromUint8(**(**Tu8)(__ccgo_up(pCell + 1)))&libc.Int32FromInt32(0x80) != 0); v2 {
					v1 = nCell&libc.Int32FromInt32(0x7f)<<libc.Int32FromInt32(7) + libc.Int32FromUint8(**(**Tu8)(__ccgo_up(pCell + 1)))
					nCell = v1
				}
				if v2 && v1 <= libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaxLocal) {
					/* The record-size field is a 2 byte varint and the record
					 ** fits entirely on the main b-tree page.  */
					c1 = (*(*func(*libc.TLS, int32, uintptr, uintptr) int32)(unsafe.Pointer(&struct{ uintptr }{xRecordCompare})))(tls, nCell, pCell+2, pIdxKey)
				} else {
					pCellBody = pCell - uintptr((*TMemPage)(unsafe.Pointer(pPage)).FchildPtrSize)
					nOverrun = int32(18) /* Size of the overrun padding */
					(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxParseCell})))(tls, pPage, pCellBody, pCur+48)
					nCell = int32((*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey)
					/* True if key size is 2^32 or more */
					/* Invalid key size:  0x80 0x80 0x00 */
					/* Invalid key size:  0x80 0x80 0x01 */
					/* Minimum legal index key size */
					if nCell < int32(2) || libc.Uint32FromInt32(nCell)/(*TBtShared)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpBt)).FusableSize > (*TBtShared)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpBt)).FnPage {
						rc = _sqlite3CorruptError(tls, int32(79314))
						goto moveto_index_finish
					}
					pCellKey = _sqlite3Malloc(tls, libc.Uint64FromInt32(nCell)+libc.Uint64FromInt32(nOverrun))
					if pCellKey == uintptr(0) {
						rc = int32(SQLITE_NOMEM)
						goto moveto_index_finish
					}
					(*TBtCursor)(unsafe.Pointer(pCur)).Fix = libc.Uint16FromInt32(idx)
					rc = _accessPayload(tls, pCur, uint32(0), libc.Uint32FromInt32(nCell), pCellKey, 0)
					libc.Xmemset(tls, pCellKey+uintptr(nCell), 0, libc.Uint64FromInt32(nOverrun)) /* Fix uninit warnings */
					v3 = pCur + 1
					*(*Tu8)(unsafe.Pointer(v3)) = Tu8(int32(*(*Tu8)(unsafe.Pointer(v3))) & ^libc.Int32FromInt32(BTCF_ValidOvfl))
					if rc != 0 {
						Xsqlite3_free(tls, pCellKey)
						goto moveto_index_finish
					}
					c1 = _sqlite3VdbeRecordCompare(tls, nCell, pCellKey, pIdxKey)
					Xsqlite3_free(tls, pCellKey)
				}
			}
			if c1 < 0 {
				lwr = idx + int32(1)
			} else {
				if c1 > 0 {
					upr = idx - int32(1)
				} else {
					**(**int32)(__ccgo_up(pRes)) = 0
					rc = SQLITE_OK
					(*TBtCursor)(unsafe.Pointer(pCur)).Fix = libc.Uint16FromInt32(idx)
					if (*TUnpackedRecord)(unsafe.Pointer(pIdxKey)).FerrCode != 0 {
						rc = _sqlite3CorruptError(tls, int32(79346))
					}
					goto moveto_index_finish
				}
			}
			if lwr > upr {
				break
			}
			idx = (lwr + upr) >> int32(1) /* idx = (lwr+upr)/2 */
			goto _5
		_5:
		}
		if (*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0 {
			(*TBtCursor)(unsafe.Pointer(pCur)).Fix = libc.Uint16FromInt32(idx)
			**(**int32)(__ccgo_up(pRes)) = c1
			rc = SQLITE_OK
			goto moveto_index_finish
		}
		if lwr >= libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) {
			chldPg = _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pPage)).FhdrOffset)+int32(8)))
		} else {
			chldPg = _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*lwr))))))
		}
		/* This block is similar to an in-lined version of:
		 **
		 **    pCur->ix = (u16)lwr;
		 **    rc = moveToChild(pCur, chldPg);
		 **    if( rc ) break;
		 */
		(*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnSize = uint16(0)
		v3 = pCur + 1
		*(*Tu8)(unsafe.Pointer(v3)) = Tu8(int32(*(*Tu8)(unsafe.Pointer(v3))) & ^(libc.Int32FromInt32(BTCF_ValidNKey) | libc.Int32FromInt32(BTCF_ValidOvfl)))
		if int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage) >= libc.Int32FromInt32(BTCURSOR_MAX_DEPTH)-libc.Int32FromInt32(1) {
			return _sqlite3CorruptError(tls, int32(79377))
		}
		**(**Tu16)(__ccgo_up(pCur + 88 + uintptr((*TBtCursor)(unsafe.Pointer(pCur)).FiPage)*2)) = libc.Uint16FromInt32(lwr)
		**(**uintptr)(__ccgo_up(pCur + 144 + uintptr((*TBtCursor)(unsafe.Pointer(pCur)).FiPage)*8)) = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
		(*TBtCursor)(unsafe.Pointer(pCur)).Fix = uint16(0)
		(*TBtCursor)(unsafe.Pointer(pCur)).FiPage = (*TBtCursor)(unsafe.Pointer(pCur)).FiPage + 1
		rc = _getAndInitPage(tls, (*TBtCursor)(unsafe.Pointer(pCur)).FpBt, chldPg, pCur+136, libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FcurPagerFlags))
		if rc == SQLITE_OK && (libc.Int32FromUint16((*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).FnCell) < int32(1) || libc.Int32FromUint8((*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).FintKey) != libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FcurIntKey)) {
			_releasePage(tls, (*TBtCursor)(unsafe.Pointer(pCur)).FpPage)
			rc = _sqlite3CorruptError(tls, int32(79388))
		}
		if rc != 0 {
			v3 = pCur + 84
			*(*Ti8)(unsafe.Pointer(v3)) = *(*Ti8)(unsafe.Pointer(v3)) - 1
			v10 = *(*Ti8)(unsafe.Pointer(v3))
			(*TBtCursor)(unsafe.Pointer(pCur)).FpPage = **(**uintptr)(__ccgo_up(pCur + 144 + uintptr(v10)*8))
			break
		}
		/*
		 ***** End of in-lined moveToChild() call */
		goto _4
	_4:
	}
	goto moveto_index_finish
moveto_index_finish:
	;
	(*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnSize = uint16(0)
	return rc
}

// C documentation
//
//	/*
//	** Insert a new record into the BTree.  The content of the new record
//	** is described by the pX object.  The pCur cursor is used only to
//	** define what table the record should be inserted into, and is left
//	** pointing at a random location.
//	**
//	** For a table btree (used for rowid tables), only the pX.nKey value of
//	** the key is used. The pX.pKey value must be NULL.  The pX.nKey is the
//	** rowid or INTEGER PRIMARY KEY of the row.  The pX.nData,pData,nZero fields
//	** hold the content of the row.
//	**
//	** For an index btree (used for indexes and WITHOUT ROWID tables), the
//	** key is an arbitrary byte sequence stored in pX.pKey,nKey.  The
//	** pX.pData,nData,nZero fields must be zero.
//	**
//	** If the seekResult parameter is non-zero, then a successful call to
//	** sqlite3BtreeIndexMoveto() to seek cursor pCur to (pKey,nKey) has already
//	** been performed.  In other words, if seekResult!=0 then the cursor
//	** is currently pointing to a cell that will be adjacent to the cell
//	** to be inserted.  If seekResult<0 then pCur points to a cell that is
//	** smaller then (pKey,nKey).  If seekResult>0 then pCur points to a cell
//	** that is larger than (pKey,nKey).
//	**
//	** If seekResult==0, that means pCur is pointing at some unknown location.
//	** In that case, this routine must seek the cursor to the correct insertion
//	** point for (pKey,nKey) before doing the insertion.  For index btrees,
//	** if pX->nMem is non-zero, then pX->aMem contains pointers to the unpacked
//	** key values and pX->aMem can be used instead of pX->pKey to avoid having
//	** to decode the key.
//	*/
func _sqlite3BtreeInsert(tls *libc.TLS, pCur uintptr, pX uintptr, flags int32, seekResult int32) (r int32) {
	bp := tls.Alloc(160)
	defer tls.Free(160)
	var idx int32
	var newCell, oldCell, p, pPage, v1 uintptr
	var ovfl TPgno
	var v2 Tu16
	var _ /* info at bp+104 */ TCellInfo
	var _ /* info at bp+128 */ TCellInfo
	var _ /* loc at bp+4 */ int32
	var _ /* r at bp+16 */ TUnpackedRecord
	var _ /* rc at bp+0 */ int32
	var _ /* szNew at bp+8 */ int32
	var _ /* x2 at bp+56 */ TBtreePayload
	_, _, _, _, _, _, _, _ = idx, newCell, oldCell, ovfl, p, pPage, v1, v2
	**(**int32)(__ccgo_up(bp + 4)) = seekResult /* -1: before desired location  +1: after */
	**(**int32)(__ccgo_up(bp + 8)) = 0
	p = (*TBtCursor)(unsafe.Pointer(pCur)).FpBtree
	newCell = uintptr(0)
	/* Save the positions of any other cursors open on this table.
	 **
	 ** In some cases, the call to btreeMoveto() below is a no-op. For
	 ** example, when inserting data into a table with auto-generated integer
	 ** keys, the VDBE layer invokes sqlite3BtreeLast() to figure out the
	 ** integer key to use. It then calls this function to actually insert the
	 ** data into the intkey B-Tree. In this case btreeMoveto() recognizes
	 ** that the cursor is already where it needs to be and returns without
	 ** doing any work. To avoid thwarting these optimizations, it is important
	 ** not to clear the cursor here.
	 */
	if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FcurFlags)&int32(BTCF_Multiple) != 0 {
		**(**int32)(__ccgo_up(bp)) = _saveAllCursors(tls, (*TBtree)(unsafe.Pointer(p)).FpBt, (*TBtCursor)(unsafe.Pointer(pCur)).FpgnoRoot, pCur)
		if **(**int32)(__ccgo_up(bp)) != 0 {
			return **(**int32)(__ccgo_up(bp))
		}
		if **(**int32)(__ccgo_up(bp + 4)) != 0 && int32((*TBtCursor)(unsafe.Pointer(pCur)).FiPage) < 0 {
			/* This can only happen if the schema is corrupt such that there is more
			 ** than one table or index with the same root page as used by the cursor.
			 ** Which can only happen if the SQLITE_NoSchemaError flag was set when
			 ** the schema was loaded. This cannot be asserted though, as a user might
			 ** set the flag, load the schema, and then unset the flag.  */
			return _sqlite3CorruptError(tls, int32(82581))
		}
	}
	/* Ensure that the cursor is not in the CURSOR_FAULT state and that it
	 ** points to a valid cell.
	 */
	if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) >= int32(CURSOR_REQUIRESEEK) {
		**(**int32)(__ccgo_up(bp)) = _moveToRoot(tls, pCur)
		if **(**int32)(__ccgo_up(bp)) != 0 && **(**int32)(__ccgo_up(bp)) != int32(SQLITE_EMPTY) {
			return **(**int32)(__ccgo_up(bp))
		}
	}
	/* Assert that the caller has been consistent. If this cursor was opened
	 ** expecting an index b-tree, then the caller should be inserting blob
	 ** keys with no associated data. If the cursor was opened expecting an
	 ** intkey table, the caller should be inserting integer keys with a
	 ** blob of associated data.  */
	if (*TBtCursor)(unsafe.Pointer(pCur)).FpKeyInfo == uintptr(0) {
		/* If this is an insert into a table b-tree, invalidate any incrblob
		 ** cursors open on the row being replaced */
		if (*TBtree)(unsafe.Pointer(p)).FhasIncrblobCur != 0 {
			_invalidateIncrblobCursors(tls, p, (*TBtCursor)(unsafe.Pointer(pCur)).FpgnoRoot, (*TBtreePayload)(unsafe.Pointer(pX)).FnKey, 0)
		}
		/* If BTREE_SAVEPOSITION is set, the cursor must already be pointing
		 ** to a row with the same key as the new entry being inserted.
		 */
		/* On the other hand, BTREE_SAVEPOSITION==0 does not imply
		 ** that the cursor is not pointing to a row to be overwritten.
		 ** So do a complete check.
		 */
		if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FcurFlags)&int32(BTCF_ValidNKey) != 0 && (*TBtreePayload)(unsafe.Pointer(pX)).FnKey == (*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey {
			/* The cursor is pointing to the entry that is to be
			 ** overwritten */
			if libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnSize) != 0 && (*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnPayload == libc.Uint32FromInt32((*TBtreePayload)(unsafe.Pointer(pX)).FnData)+libc.Uint32FromInt32((*TBtreePayload)(unsafe.Pointer(pX)).FnZero) {
				/* New entry is the same size as the old.  Do an overwrite */
				return _btreeOverwriteCell(tls, pCur, pX)
			}
		} else {
			if **(**int32)(__ccgo_up(bp + 4)) == 0 {
				/* The cursor is *not* pointing to the cell to be overwritten, nor
				 ** to an adjacent cell.  Move the cursor so that it is pointing either
				 ** to the cell to be overwritten or an adjacent cell.
				 */
				**(**int32)(__ccgo_up(bp)) = _sqlite3BtreeTableMoveto(tls, pCur, (*TBtreePayload)(unsafe.Pointer(pX)).FnKey, libc.BoolInt32(flags&int32(BTREE_APPEND) != 0), bp+4)
				if **(**int32)(__ccgo_up(bp)) != 0 {
					return **(**int32)(__ccgo_up(bp))
				}
			}
		}
	} else {
		/* This is an index or a WITHOUT ROWID table */
		/* If BTREE_SAVEPOSITION is set, the cursor must already be pointing
		 ** to a row with the same key as the new entry being inserted.
		 */
		/* If the cursor is not already pointing either to the cell to be
		 ** overwritten, or if a new cell is being inserted, if the cursor is
		 ** not pointing to an immediately adjacent cell, then move the cursor
		 ** so that it does.
		 */
		if **(**int32)(__ccgo_up(bp + 4)) == 0 && flags&int32(BTREE_SAVEPOSITION) == 0 {
			if (*TBtreePayload)(unsafe.Pointer(pX)).FnMem != 0 {
				(**(**TUnpackedRecord)(__ccgo_up(bp + 16))).FpKeyInfo = (*TBtCursor)(unsafe.Pointer(pCur)).FpKeyInfo
				(**(**TUnpackedRecord)(__ccgo_up(bp + 16))).FaMem = (*TBtreePayload)(unsafe.Pointer(pX)).FaMem
				(**(**TUnpackedRecord)(__ccgo_up(bp + 16))).FnField = (*TBtreePayload)(unsafe.Pointer(pX)).FnMem
				(**(**TUnpackedRecord)(__ccgo_up(bp + 16))).Fdefault_rc = 0
				(**(**TUnpackedRecord)(__ccgo_up(bp + 16))).FeqSeen = uint8(0)
				**(**int32)(__ccgo_up(bp)) = _sqlite3BtreeIndexMoveto(tls, pCur, bp+16, bp+4)
			} else {
				**(**int32)(__ccgo_up(bp)) = _btreeMoveto(tls, pCur, (*TBtreePayload)(unsafe.Pointer(pX)).FpKey, (*TBtreePayload)(unsafe.Pointer(pX)).FnKey, libc.BoolInt32(flags&int32(BTREE_APPEND) != 0), bp+4)
			}
			if **(**int32)(__ccgo_up(bp)) != 0 {
				return **(**int32)(__ccgo_up(bp))
			}
		}
		/* If the cursor is currently pointing to an entry to be overwritten
		 ** and the new content is the same as as the old, then use the
		 ** overwrite optimization.
		 */
		if **(**int32)(__ccgo_up(bp + 4)) == 0 {
			_getCellInfo(tls, pCur)
			if (*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey == (*TBtreePayload)(unsafe.Pointer(pX)).FnKey {
				(**(**TBtreePayload)(__ccgo_up(bp + 56))).FpData = (*TBtreePayload)(unsafe.Pointer(pX)).FpKey
				(**(**TBtreePayload)(__ccgo_up(bp + 56))).FnData = int32((*TBtreePayload)(unsafe.Pointer(pX)).FnKey)
				(**(**TBtreePayload)(__ccgo_up(bp + 56))).FnZero = 0
				return _btreeOverwriteCell(tls, pCur, bp+56)
			}
		}
	}
	pPage = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage
	if (*TMemPage)(unsafe.Pointer(pPage)).FnFree < 0 {
		if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) > int32(CURSOR_INVALID) {
			/* ^^^^^--- due to the moveToRoot() call above */
			**(**int32)(__ccgo_up(bp)) = _sqlite3CorruptError(tls, int32(82704))
		} else {
			**(**int32)(__ccgo_up(bp)) = _btreeComputeFreeSpace(tls, pPage)
		}
		if **(**int32)(__ccgo_up(bp)) != 0 {
			return **(**int32)(__ccgo_up(bp))
		}
	}
	newCell = (*TBtShared)(unsafe.Pointer((*TBtree)(unsafe.Pointer(p)).FpBt)).FpTmpSpace
	if flags&int32(BTREE_PREFORMAT) != 0 {
		**(**int32)(__ccgo_up(bp)) = SQLITE_OK
		**(**int32)(__ccgo_up(bp + 8)) = (*TBtShared)(unsafe.Pointer((*TBtree)(unsafe.Pointer(p)).FpBt)).FnPreformatSize
		if **(**int32)(__ccgo_up(bp + 8)) < int32(4) {
			**(**int32)(__ccgo_up(bp + 8)) = int32(4)
			**(**uint8)(__ccgo_up(newCell + 3)) = uint8(0)
		}
		if (*TBtShared)(unsafe.Pointer((*TBtree)(unsafe.Pointer(p)).FpBt)).FautoVacuum != 0 && **(**int32)(__ccgo_up(bp + 8)) > libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaxLocal) {
			(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxParseCell})))(tls, pPage, newCell, bp+104)
			if (**(**TCellInfo)(__ccgo_up(bp + 104))).FnPayload != uint32((**(**TCellInfo)(__ccgo_up(bp + 104))).FnLocal) {
				ovfl = _sqlite3Get4byte(tls, newCell+uintptr(**(**int32)(__ccgo_up(bp + 8))-int32(4)))
				_ptrmapPut(tls, (*TBtree)(unsafe.Pointer(p)).FpBt, ovfl, uint8(PTRMAP_OVERFLOW1), (*TMemPage)(unsafe.Pointer(pPage)).Fpgno, bp)
				if **(**int32)(__ccgo_up(bp)) != 0 {
					goto end_insert
				}
			}
		}
	} else {
		**(**int32)(__ccgo_up(bp)) = _fillInCell(tls, pPage, newCell, pX, bp+8)
		if **(**int32)(__ccgo_up(bp)) != 0 {
			goto end_insert
		}
	}
	idx = libc.Int32FromUint16((*TBtCursor)(unsafe.Pointer(pCur)).Fix)
	(*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnSize = uint16(0)
	if **(**int32)(__ccgo_up(bp + 4)) == 0 {
		if idx >= libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) {
			return _sqlite3CorruptError(tls, int32(82746))
		}
		**(**int32)(__ccgo_up(bp)) = _sqlite3PagerWrite(tls, (*TMemPage)(unsafe.Pointer(pPage)).FpDbPage)
		if **(**int32)(__ccgo_up(bp)) != 0 {
			goto end_insert
		}
		oldCell = (*TMemPage)(unsafe.Pointer(pPage)).FaData + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*idx)))))
		if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
			libc.Xmemcpy(tls, newCell, oldCell, uint64(4))
		}
		(*(*func(*libc.TLS, uintptr, uintptr, uintptr))(unsafe.Pointer(&struct{ uintptr }{(*TMemPage)(unsafe.Pointer(pPage)).FxParseCell})))(tls, pPage, oldCell, bp+128)
		if uint32((**(**TCellInfo)(__ccgo_up(bp + 128))).FnLocal) != (**(**TCellInfo)(__ccgo_up(bp + 128))).FnPayload {
			**(**int32)(__ccgo_up(bp)) = _clearCellOverflow(tls, pPage, oldCell, bp+128)
		} else {
			**(**int32)(__ccgo_up(bp)) = SQLITE_OK
		}
		v1 = pCur + 1
		*(*Tu8)(unsafe.Pointer(v1)) = Tu8(int32(*(*Tu8)(unsafe.Pointer(v1))) & ^libc.Int32FromInt32(BTCF_ValidOvfl))
		if libc.Int32FromUint16((**(**TCellInfo)(__ccgo_up(bp + 128))).FnSize) == **(**int32)(__ccgo_up(bp + 8)) && uint32((**(**TCellInfo)(__ccgo_up(bp + 128))).FnLocal) == (**(**TCellInfo)(__ccgo_up(bp + 128))).FnPayload && (!((*TBtShared)(unsafe.Pointer((*TBtree)(unsafe.Pointer(p)).FpBt)).FautoVacuum != 0) || **(**int32)(__ccgo_up(bp + 8)) < libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FminLocal)) {
			/* Overwrite the old cell with the new if they are the same size.
			 ** We could also try to do this if the old cell is smaller, then add
			 ** the leftover space to the free list.  But experiments show that
			 ** doing that is no faster then skipping this optimization and just
			 ** calling dropCell() and insertCell().
			 **
			 ** This optimization cannot be used on an autovacuum database if the
			 ** new entry uses overflow pages, as the insertCell() call below is
			 ** necessary to add the PTRMAP_OVERFLOW1 pointer-map entry.  */
			/* clearCell never fails when nLocal==nPayload */
			if oldCell < (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr((*TMemPage)(unsafe.Pointer(pPage)).FhdrOffset)+uintptr(10) {
				return _sqlite3CorruptError(tls, int32(82773))
			}
			if oldCell+uintptr(**(**int32)(__ccgo_up(bp + 8))) > (*TMemPage)(unsafe.Pointer(pPage)).FaDataEnd {
				return _sqlite3CorruptError(tls, int32(82776))
			}
			libc.Xmemcpy(tls, oldCell, newCell, libc.Uint64FromInt32(**(**int32)(__ccgo_up(bp + 8))))
			return SQLITE_OK
		}
		_dropCell(tls, pPage, idx, libc.Int32FromUint16((**(**TCellInfo)(__ccgo_up(bp + 128))).FnSize), bp)
		if **(**int32)(__ccgo_up(bp)) != 0 {
			goto end_insert
		}
	} else {
		if **(**int32)(__ccgo_up(bp + 4)) < 0 && libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) > 0 {
			v1 = pCur + 86
			*(*Tu16)(unsafe.Pointer(v1)) = *(*Tu16)(unsafe.Pointer(v1)) + 1
			v2 = *(*Tu16)(unsafe.Pointer(v1))
			idx = libc.Int32FromUint16(v2)
			v1 = pCur + 1
			*(*Tu8)(unsafe.Pointer(v1)) = Tu8(int32(*(*Tu8)(unsafe.Pointer(v1))) & ^(libc.Int32FromInt32(BTCF_ValidNKey) | libc.Int32FromInt32(BTCF_ValidOvfl)))
		} else {
		}
	}
	**(**int32)(__ccgo_up(bp)) = _insertCellFast(tls, pPage, idx, newCell, **(**int32)(__ccgo_up(bp + 8)))
	/* If no error has occurred and pPage has an overflow cell, call balance()
	 ** to redistribute the cells within the tree. Since balance() may move
	 ** the cursor, zero the BtCursor.info.nSize and BTCF_ValidNKey
	 ** variables.
	 **
	 ** Previous versions of SQLite called moveToRoot() to move the cursor
	 ** back to the root page as balance() used to invalidate the contents
	 ** of BtCursor.apPage[] and BtCursor.aiIdx[]. Instead of doing that,
	 ** set the cursor state to "invalid". This makes common insert operations
	 ** slightly faster.
	 **
	 ** There is a subtle but important optimization here too. When inserting
	 ** multiple records into an intkey b-tree using a single cursor (as can
	 ** happen while processing an "INSERT INTO ... SELECT" statement), it
	 ** is advantageous to leave the cursor pointing to the last entry in
	 ** the b-tree if possible. If the cursor is left pointing to the last
	 ** entry in the table, and the next row inserted has an integer key
	 ** larger than the largest existing key, it is possible to insert the
	 ** row without seeking the cursor. This can be a big performance boost.
	 */
	if (*TMemPage)(unsafe.Pointer(pPage)).FnOverflow != 0 {
		v1 = pCur + 1
		*(*Tu8)(unsafe.Pointer(v1)) = Tu8(int32(*(*Tu8)(unsafe.Pointer(v1))) & ^(libc.Int32FromInt32(BTCF_ValidNKey) | libc.Int32FromInt32(BTCF_ValidOvfl)))
		**(**int32)(__ccgo_up(bp)) = _balance(tls, pCur)
		/* Must make sure nOverflow is reset to zero even if the balance()
		 ** fails. Internal data structure corruption will result otherwise.
		 ** Also, set the cursor state to invalid. This stops saveCursorPosition()
		 ** from trying to save the current position of the cursor.  */
		(*TMemPage)(unsafe.Pointer((*TBtCursor)(unsafe.Pointer(pCur)).FpPage)).FnOverflow = uint8(0)
		(*TBtCursor)(unsafe.Pointer(pCur)).FeState = uint8(CURSOR_INVALID)
		if flags&int32(BTREE_SAVEPOSITION) != 0 && **(**int32)(__ccgo_up(bp)) == SQLITE_OK {
			_btreeReleaseAllCursorPages(tls, pCur)
			if (*TBtCursor)(unsafe.Pointer(pCur)).FpKeyInfo != 0 {
				(*TBtCursor)(unsafe.Pointer(pCur)).FpKey = _sqlite3Malloc(tls, libc.Uint64FromInt64((*TBtreePayload)(unsafe.Pointer(pX)).FnKey))
				if (*TBtCursor)(unsafe.Pointer(pCur)).FpKey == uintptr(0) {
					**(**int32)(__ccgo_up(bp)) = int32(SQLITE_NOMEM)
				} else {
					libc.Xmemcpy(tls, (*TBtCursor)(unsafe.Pointer(pCur)).FpKey, (*TBtreePayload)(unsafe.Pointer(pX)).FpKey, libc.Uint64FromInt64((*TBtreePayload)(unsafe.Pointer(pX)).FnKey))
				}
			}
			(*TBtCursor)(unsafe.Pointer(pCur)).FeState = uint8(CURSOR_REQUIRESEEK)
			(*TBtCursor)(unsafe.Pointer(pCur)).FnKey = (*TBtreePayload)(unsafe.Pointer(pX)).FnKey
		}
	}
	goto end_insert
end_insert:
	;
	return **(**int32)(__ccgo_up(bp))
	return r
}

// C documentation
//
//	/* Move the cursor so that it points to an entry in a table (a.k.a INTKEY)
//	** table near the key intKey.   Return a success code.
//	**
//	** If an exact match is not found, then the cursor is always
//	** left pointing at a leaf page which would hold the entry if it
//	** were present.  The cursor might point to an entry that comes
//	** before or after the key.
//	**
//	** An integer is written into *pRes which is the result of
//	** comparing the key with the entry to which the cursor is
//	** pointing.  The meaning of the integer written into
//	** *pRes is as follows:
//	**
//	**     *pRes<0      The cursor is left pointing at an entry that
//	**                  is smaller than intKey or if the table is empty
//	**                  and the cursor is therefore left point to nothing.
//	**
//	**     *pRes==0     The cursor is left pointing at an entry that
//	**                  exactly matches intKey.
//	**
//	**     *pRes>0      The cursor is left pointing at an entry that
//	**                  is larger than intKey.
//	*/
func _sqlite3BtreeTableMoveto(tls *libc.TLS, pCur uintptr, intKey Ti64, biasRight int32, pRes uintptr) (r int32) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var c, idx, lwr, rc, upr int32
	var chldPg TPgno
	var pCell, pPage, v3 uintptr
	var _ /* nCellKey at bp+0 */ Ti64
	_, _, _, _, _, _, _, _, _ = c, chldPg, idx, lwr, pCell, pPage, rc, upr, v3
	/* If the cursor is already positioned at the point we are trying
	 ** to move to, then just return without doing any work */
	if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FeState) == CURSOR_VALID && libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FcurFlags)&int32(BTCF_ValidNKey) != 0 {
		if (*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey == intKey {
			**(**int32)(__ccgo_up(pRes)) = 0
			return SQLITE_OK
		}
		if (*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey < intKey {
			if libc.Int32FromUint8((*TBtCursor)(unsafe.Pointer(pCur)).FcurFlags)&int32(BTCF_AtLast) != 0 {
				**(**int32)(__ccgo_up(pRes)) = -int32(1)
				return SQLITE_OK
			}
			/* If the requested key is one more than the previous key, then
			 ** try to get there using sqlite3BtreeNext() rather than a full
			 ** binary search.  This is an optimization only.  The correct answer
			 ** is still obtained without this case, only a little more slowly. */
			if (*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey+int64(1) == intKey {
				**(**int32)(__ccgo_up(pRes)) = 0
				rc = _sqlite3BtreeNext(tls, pCur, 0)
				if rc == SQLITE_OK {
					_getCellInfo(tls, pCur)
					if (*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey == intKey {
						return SQLITE_OK
					}
				} else {
					if rc != int32(SQLITE_DONE) {
						return rc
					}
				}
			}
		}
	}
	rc = _moveToRoot(tls, pCur)
	if rc != 0 {
		if rc == int32(SQLITE_EMPTY) {
			**(**int32)(__ccgo_up(pRes)) = -int32(1)
			return SQLITE_OK
		}
		return rc
	}
	for {
		pPage = (*TBtCursor)(unsafe.Pointer(pCur)).FpPage /* Pointer to current cell in pPage */
		/* pPage->nCell must be greater than zero. If this is the root-page
		 ** the cursor would have been INVALID above and this for(;;) loop
		 ** not run. If this is not the root-page, then the moveToChild() routine
		 ** would have already detected db corruption. Similarly, pPage must
		 ** be the right kind (index or table) of b-tree page. Otherwise
		 ** a moveToChild() or moveToRoot() call would have detected corruption.  */
		lwr = 0
		upr = libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) - int32(1)
		idx = upr >> (int32(1) - biasRight) /* idx = biasRight ? upr : (lwr+upr)/2; */
		for {
			pCell = (*TMemPage)(unsafe.Pointer(pPage)).FaDataOfst + uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*idx)))))
			if (*TMemPage)(unsafe.Pointer(pPage)).FintKeyLeaf != 0 {
				for {
					v3 = pCell
					pCell = pCell + 1
					if !(int32(0x80) <= libc.Int32FromUint8(**(**Tu8)(__ccgo_up(v3)))) {
						break
					}
					if pCell >= (*TMemPage)(unsafe.Pointer(pPage)).FaDataEnd {
						return _sqlite3CorruptError(tls, int32(79032))
					}
				}
			}
			_sqlite3GetVarint(tls, pCell, bp)
			if **(**Ti64)(__ccgo_up(bp)) < intKey {
				lwr = idx + int32(1)
				if lwr > upr {
					c = -int32(1)
					break
				}
			} else {
				if **(**Ti64)(__ccgo_up(bp)) > intKey {
					upr = idx - int32(1)
					if lwr > upr {
						c = +libc.Int32FromInt32(1)
						break
					}
				} else {
					(*TBtCursor)(unsafe.Pointer(pCur)).Fix = libc.Uint16FromInt32(idx)
					if !((*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0) {
						lwr = idx
						goto moveto_table_next_layer
					} else {
						v3 = pCur + 1
						*(*Tu8)(unsafe.Pointer(v3)) = Tu8(int32(*(*Tu8)(unsafe.Pointer(v3))) | libc.Int32FromInt32(BTCF_ValidNKey))
						(*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnKey = **(**Ti64)(__ccgo_up(bp))
						(*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnSize = uint16(0)
						**(**int32)(__ccgo_up(pRes)) = 0
						return SQLITE_OK
					}
				}
			}
			idx = (lwr + upr) >> int32(1) /* idx = (lwr+upr)/2; */
			goto _2
		_2:
		}
		if (*TMemPage)(unsafe.Pointer(pPage)).Fleaf != 0 {
			(*TBtCursor)(unsafe.Pointer(pCur)).Fix = libc.Uint16FromInt32(idx)
			**(**int32)(__ccgo_up(pRes)) = c
			rc = SQLITE_OK
			goto moveto_table_finish
		}
		goto moveto_table_next_layer
	moveto_table_next_layer:
		;
		if lwr >= libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FnCell) {
			chldPg = _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint8((*TMemPage)(unsafe.Pointer(pPage)).FhdrOffset)+int32(8)))
		} else {
			chldPg = _sqlite3Get4byte(tls, (*TMemPage)(unsafe.Pointer(pPage)).FaData+uintptr(libc.Int32FromUint16((*TMemPage)(unsafe.Pointer(pPage)).FmaskPage)&libc.Int32FromUint16(**(**Tu16)(__ccgo_up((*TMemPage)(unsafe.Pointer(pPage)).FaCellIdx + uintptr(int32(2)*lwr))))))
		}
		(*TBtCursor)(unsafe.Pointer(pCur)).Fix = libc.Uint16FromInt32(lwr)
		rc = _moveToChild(tls, pCur, chldPg)
		if rc != 0 {
			break
		}
		goto _1
	_1:
	}
	goto moveto_table_finish
moveto_table_finish:
	;
	(*TBtCursor)(unsafe.Pointer(pCur)).Finfo.FnSize = uint16(0)
	return rc
}

// C documentation
//
//	/*
//	** This function is exactly the same as sqlite3_create_function(), except
//	** that it is designed to be called by internal code. The difference is
//	** that if a malloc() fails in sqlite3_create_function(), an error code
//	** is returned and the mallocFailed flag cleared.
//	*/
func _sqlite3CreateFunc(tls *libc.TLS, db uintptr, zFunctionName uintptr, nArg int32, enc int32, pUserData uintptr, __ccgo_fp_xSFunc uintptr, __ccgo_fp_xStep uintptr, __ccgo_fp_xFinal uintptr, __ccgo_fp_xValue uintptr, __ccgo_fp_xInverse uintptr, pDestructor uintptr) (r int32) {
	var extraFlags, rc int32
	var p, v1 uintptr
	_, _, _, _ = extraFlags, p, rc, v1
	if zFunctionName == uintptr(0) || __ccgo_fp_xSFunc != uintptr(0) && __ccgo_fp_xFinal != uintptr(0) || libc.BoolInt32(__ccgo_fp_xFinal == uintptr(0)) != libc.BoolInt32(__ccgo_fp_xStep == uintptr(0)) || libc.BoolInt32(__ccgo_fp_xValue == uintptr(0)) != libc.BoolInt32(__ccgo_fp_xInverse == uintptr(0)) || (nArg < -int32(1) || nArg > int32(SQLITE_MAX_FUNCTION_ARG)) || int32(255) < _sqlite3Strlen30(tls, zFunctionName) {
		return _sqlite3MisuseError(tls, int32(189155))
	}
	extraFlags = enc & (libc.Int32FromInt32(SQLITE_DETERMINISTIC) | libc.Int32FromInt32(SQLITE_DIRECTONLY) | libc.Int32FromInt32(SQLITE_SUBTYPE) | libc.Int32FromInt32(SQLITE_INNOCUOUS) | libc.Int32FromInt32(SQLITE_RESULT_SUBTYPE) | libc.Int32FromInt32(SQLITE_SELFORDER1))
	enc = enc & (libc.Int32FromInt32(SQLITE_FUNC_ENCMASK) | libc.Int32FromInt32(SQLITE_ANY))
	/* The SQLITE_INNOCUOUS flag is the same bit as SQLITE_FUNC_UNSAFE.  But
	 ** the meaning is inverted.  So flip the bit. */
	extraFlags = extraFlags ^ int32(SQLITE_FUNC_UNSAFE) /* tag-20230109-1 */
	/* If SQLITE_UTF16 is specified as the encoding type, transform this
	 ** to one of SQLITE_UTF16LE or SQLITE_UTF16BE using the
	 ** SQLITE_UTF16NATIVE macro. SQLITE_UTF16 is not used internally.
	 **
	 ** If SQLITE_ANY is specified, add three versions of the function
	 ** to the hash table.
	 */
	switch enc {
	case int32(SQLITE_UTF16):
		enc = int32(SQLITE_UTF16BE)
	case int32(SQLITE_ANY):
		rc = _sqlite3CreateFunc(tls, db, zFunctionName, nArg, int32(SQLITE_UTF8)|extraFlags^int32(SQLITE_FUNC_UNSAFE), pUserData, __ccgo_fp_xSFunc, __ccgo_fp_xStep, __ccgo_fp_xFinal, __ccgo_fp_xValue, __ccgo_fp_xInverse, pDestructor)
		if rc == SQLITE_OK {
			rc = _sqlite3CreateFunc(tls, db, zFunctionName, nArg, int32(SQLITE_UTF16LE)|extraFlags^int32(SQLITE_FUNC_UNSAFE), pUserData, __ccgo_fp_xSFunc, __ccgo_fp_xStep, __ccgo_fp_xFinal, __ccgo_fp_xValue, __ccgo_fp_xInverse, pDestructor)
		}
		if rc != SQLITE_OK {
			return rc
		}
		enc = int32(SQLITE_UTF16BE)
	case int32(SQLITE_UTF8):
		fallthrough
	case int32(SQLITE_UTF16LE):
		fallthrough
	case int32(SQLITE_UTF16BE):
	default:
		enc = int32(SQLITE_UTF8)
		break
	}
	/* Check if an existing function is being overridden or deleted. If so,
	 ** and there are active VMs, then return SQLITE_BUSY. If a function
	 ** is being overridden/deleted but there are no active VMs, allow the
	 ** operation to continue but invalidate all precompiled statements.
	 */
	p = _sqlite3FindFunction(tls, db, zFunctionName, nArg, libc.Uint8FromInt32(enc), uint8(0))
	if p != 0 && (*TFuncDef)(unsafe.Pointer(p)).FfuncFlags&uint32(SQLITE_FUNC_ENCMASK) == libc.Uint32FromInt32(enc) && int32((*TFuncDef)(unsafe.Pointer(p)).FnArg) == nArg {
		if (*Tsqlite3)(unsafe.Pointer(db)).FnVdbeActive != 0 {
			_sqlite3ErrorWithMsg(tls, db, int32(SQLITE_BUSY), __ccgo_ts+25846, 0)
			return int32(SQLITE_BUSY)
		} else {
			_sqlite3ExpirePreparedStatements(tls, db, 0)
		}
	} else {
		if __ccgo_fp_xSFunc == uintptr(0) && __ccgo_fp_xFinal == uintptr(0) {
			/* Trying to delete a function that does not exist.  This is a no-op.
			 ** https://sqlite.org/forum/forumpost/726219164b */
			return SQLITE_OK
		}
	}
	p = _sqlite3FindFunction(tls, db, zFunctionName, nArg, libc.Uint8FromInt32(enc), uint8(1))
	if !(p != 0) {
		return int32(SQLITE_NOMEM)
	}
	/* If an older version of the function with a configured destructor is
	 ** being replaced invoke the destructor function here. */
	_functionDestroy(tls, db, p)
	if pDestructor != 0 {
		(*TFuncDestructor)(unsafe.Pointer(pDestructor)).FnRef = (*TFuncDestructor)(unsafe.Pointer(pDestructor)).FnRef + 1
	}
	*(*uintptr)(unsafe.Pointer(p + 64)) = pDestructor
	(*TFuncDef)(unsafe.Pointer(p)).FfuncFlags = (*TFuncDef)(unsafe.Pointer(p)).FfuncFlags&uint32(SQLITE_FUNC_ENCMASK) | libc.Uint32FromInt32(extraFlags)
	if __ccgo_fp_xSFunc != 0 {
		v1 = __ccgo_fp_xSFunc
	} else {
		v1 = __ccgo_fp_xStep
	}
	(*TFuncDef)(unsafe.Pointer(p)).FxSFunc = v1
	(*TFuncDef)(unsafe.Pointer(p)).FxFinalize = __ccgo_fp_xFinal
	(*TFuncDef)(unsafe.Pointer(p)).FxValue = __ccgo_fp_xValue
	(*TFuncDef)(unsafe.Pointer(p)).FxInverse = __ccgo_fp_xInverse
	(*TFuncDef)(unsafe.Pointer(p)).FpUserData = pUserData
	(*TFuncDef)(unsafe.Pointer(p)).FnArg = libc.Int16FromUint16(libc.Uint16FromInt32(nArg))
	return SQLITE_OK
}

// C documentation
//
//	/*
//	** Read or write a four-byte big-endian integer value.
//	*/
func _sqlite3Get4byte(tls *libc.TLS, p uintptr) (r Tu32) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var _ /* x at bp+0 */ Tu32
	libc.Xmemcpy(tls, bp, p, uint64(4))
	return **(**Tu32)(__ccgo_up(bp))
}

// C documentation
//
//	/*
//	** Process a pragma statement.
//	**
//	** Pragmas are of this form:
//	**
//	**      PRAGMA [schema.]id [= value]
//	**
//	** The identifier might also be a string.  The value is a string, and
//	** identifier, or a number.  If minusFlag is true, then the value is
//	** a number that was preceded by a minus sign.
//	**
//	** If the left side is "database.id" then pId1 is the database name
//	** and pId2 is the id.  If the left side is just "id" then pId1 is the
//	** id and pId2 is any empty string.
//	*/
func _sqlite3Pragma(tls *libc.TLS, pParse uintptr, pId1 uintptr, pId2 uintptr, pValue uintptr, minusFlag int32) {
	bp := tls.Alloc(240)
	defer tls.Free(240)
	var a1, a11, addr, addr1, addrCkFault, addrCkOk, addrOk, addrTop, b, bStrict, ckUniq, cnt, doTypeCheck, eAuto, eMode, eMode1, eMode2, i, i1, i10, i2, i3, i4, i5, i6, i7, i8, i9, iAddr, iAddr1, iBt, iCol, iCol1, iCookie, iDb, iDbLast, iEnd, iIdxDb, iLevel, iReg, iTab, iTabCur, iTabDb, iTabDb1, ii, ii1, ii2, ii3, ii4, initNCol, isHidden, isQuick, j2, j3, j4, jmp, jmp2, jmp21, jmp3, jmp4, jmp5, jmp6, jmp61, jmp7, k, k3, kk, label6, labelError, labelOk, loopTop, mx, mxCol, n, nBtree, nCheck, nHidden, nIdx, nIndex, nLimit, p11, p3, p4, r1, r11, r2, rc, regResult, regRow, showInternFunc, size, size1, size2, uniqOk, x1, v2 int32
	var aOp, aOp1, aOp2, aOp3, aOp4, aOp5, aRoot, db, j, j1, k1, k2, k4, p, p1, pBt, pBt1, pBt2, pCheck, pCol, pCol1, pColExpr, pColl, pDb, pEnc, pFK, pFK1, pHash, pIdx, pIdx1, pIdx3, pIdx4, pIdx5, pIdx6, pIdx7, pMod, pObjTab, pPager, pPager1, pParent, pPk, pPk1, pPragma, pPrior, pSchema, pTab, pTab1, pTab10, pTab11, pTab12, pTab2, pTab3, pTab4, pTab5, pTab6, pTab7, pTab8, pTab9, pTbls, pVTab, v, x2, zDb, zErr, zErr1, zErr2, zLeft, zMod, zMode, zOpt, zRet, zRight, zSql, zSubSql, zType, v1, v5 uintptr
	var azOrigin [3]uintptr
	var cnum Ti16
	var enc Tu8
	var iPrior Tsqlite3_int64
	var iRange, szThreshold TLogEst
	var mask Tu64
	var opMask Tu32
	var _ /* N at bp+136 */ Tsqlite3_int64
	var _ /* N at bp+144 */ Tsqlite3_int64
	var _ /* N at bp+152 */ Tsqlite3_int64
	var _ /* N at bp+160 */ Tsqlite3_int64
	var _ /* aFcntl at bp+8 */ [4]uintptr
	var _ /* aiCols at bp+96 */ uintptr
	var _ /* iDataCur at bp+108 */ int32
	var _ /* iIdxCur at bp+112 */ int32
	var _ /* iLimit at bp+48 */ Ti64
	var _ /* iLimit at bp+56 */ int32
	var _ /* jmp3 at bp+128 */ int32
	var _ /* mxErr at bp+104 */ int32
	var _ /* pDfltValue at bp+120 */ uintptr
	var _ /* pDummy at bp+80 */ uintptr
	var _ /* pId at bp+0 */ uintptr
	var _ /* pIdx at bp+88 */ uintptr
	var _ /* res at bp+72 */ int32
	var _ /* size at bp+60 */ int32
	var _ /* sz at bp+64 */ Tsqlite3_int64
	var _ /* x at bp+40 */ Ti64
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _ = a1, a11, aOp, aOp1, aOp2, aOp3, aOp4, aOp5, aRoot, addr, addr1, addrCkFault, addrCkOk, addrOk, addrTop, azOrigin, b, bStrict, ckUniq, cnt, cnum, db, doTypeCheck, eAuto, eMode, eMode1, eMode2, enc, i, i1, i10, i2, i3, i4, i5, i6, i7, i8, i9, iAddr, iAddr1, iBt, iCol, iCol1, iCookie, iDb, iDbLast, iEnd, iIdxDb, iLevel, iPrior, iRange, iReg, iTab, iTabCur, iTabDb, iTabDb1, ii, ii1, ii2, ii3, ii4, initNCol, isHidden, isQuick, j, j1, j2, j3, j4, jmp, jmp2, jmp21, jmp3, jmp4, jmp5, jmp6, jmp61, jmp7, k, k1, k2, k3, k4, kk, label6, labelError, labelOk, loopTop, mask, mx, mxCol, n, nBtree, nCheck, nHidden, nIdx, nIndex, nLimit, opMask, p, p1, p11, p3, p4, pBt, pBt1, pBt2, pCheck, pCol, pCol1, pColExpr, pColl, pDb, pEnc, pFK, pFK1, pHash, pIdx, pIdx1, pIdx3, pIdx4, pIdx5, pIdx6, pIdx7, pMod, pObjTab, pPager, pPager1, pParent, pPk, pPk1, pPragma, pPrior, pSchema, pTab, pTab1, pTab10, pTab11, pTab12, pTab2, pTab3, pTab4, pTab5, pTab6, pTab7, pTab8, pTab9, pTbls, pVTab, r1, r11, r2, rc, regResult, regRow, showInternFunc, size, size1, size2, szThreshold, uniqOk, v, x1, x2, zDb, zErr, zErr1, zErr2, zLeft, zMod, zMode, zOpt, zRet, zRight, zSql, zSubSql, zType, v1, v2, v5
	zLeft = uintptr(0)                         /* Nul-terminated UTF-8 string <id> */
	zRight = uintptr(0)                        /* Nul-terminated UTF-8 string <value>, or NULL */
	zDb = uintptr(0)                           /* return value form SQLITE_FCNTL_PRAGMA */
	db = (*TParse)(unsafe.Pointer(pParse)).Fdb /* The specific database being pragmaed */
	v = _sqlite3GetVdbe(tls, pParse)           /* The pragma */
	if v == uintptr(0) {
		return
	}
	_sqlite3VdbeRunOnlyOnce(tls, v)
	(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(2)
	/* Interpret the [schema.] part of the pragma statement. iDb is the
	 ** index of the database this pragma is being applied to in db.aDb[]. */
	iDb = _sqlite3TwoPartName(tls, pParse, pId1, pId2, bp)
	if iDb < 0 {
		return
	}
	pDb = (*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(iDb)*32
	/* If the temp database has been explicitly named as part of the
	 ** pragma, make sure it is open.
	 */
	if iDb == int32(1) && _sqlite3OpenTempDatabase(tls, pParse) != 0 {
		return
	}
	zLeft = _sqlite3NameFromToken(tls, db, **(**uintptr)(__ccgo_up(bp)))
	if !(zLeft != 0) {
		return
	}
	if minusFlag != 0 {
		zRight = _sqlite3MPrintf(tls, db, __ccgo_ts+19214, libc.VaList(bp+176, pValue))
	} else {
		zRight = _sqlite3NameFromToken(tls, db, pValue)
	}
	if (*TToken)(unsafe.Pointer(pId2)).Fn > uint32(0) {
		v1 = (*TDb)(unsafe.Pointer(pDb)).FzDbSName
	} else {
		v1 = uintptr(0)
	}
	zDb = v1
	if _sqlite3AuthCheck(tls, pParse, int32(SQLITE_PRAGMA), zLeft, zRight, zDb) != 0 {
		goto pragma_out
	}
	/* Send an SQLITE_FCNTL_PRAGMA file-control to the underlying VFS
	 ** connection.  If it returns SQLITE_OK, then assume that the VFS
	 ** handled the pragma and generate a no-op prepared statement.
	 **
	 ** IMPLEMENTATION-OF: R-12238-55120 Whenever a PRAGMA statement is parsed,
	 ** an SQLITE_FCNTL_PRAGMA file control is sent to the open sqlite3_file
	 ** object corresponding to the database file to which the pragma
	 ** statement refers.
	 **
	 ** IMPLEMENTATION-OF: R-29875-31678 The argument to the SQLITE_FCNTL_PRAGMA
	 ** file control is an array of pointers to strings (char**) in which the
	 ** second element of the array is the name of the pragma and the third
	 ** element is the argument to the pragma or NULL if the pragma has no
	 ** argument.
	 */
	(**(**[4]uintptr)(__ccgo_up(bp + 8)))[0] = uintptr(0)
	(**(**[4]uintptr)(__ccgo_up(bp + 8)))[int32(1)] = zLeft
	(**(**[4]uintptr)(__ccgo_up(bp + 8)))[int32(2)] = zRight
	(**(**[4]uintptr)(__ccgo_up(bp + 8)))[int32(3)] = uintptr(0)
	(*Tsqlite3)(unsafe.Pointer(db)).FbusyHandler.FnBusy = 0
	rc = Xsqlite3_file_control(tls, db, zDb, int32(SQLITE_FCNTL_PRAGMA), bp+8)
	if rc == SQLITE_OK {
		_sqlite3VdbeSetNumCols(tls, v, int32(1))
		_sqlite3VdbeSetColName(tls, v, 0, COLNAME_NAME, (**(**[4]uintptr)(__ccgo_up(bp + 8)))[0], uintptr(-libc.Int32FromInt32(1)))
		_returnSingleText(tls, v, (**(**[4]uintptr)(__ccgo_up(bp + 8)))[0])
		Xsqlite3_free(tls, (**(**[4]uintptr)(__ccgo_up(bp + 8)))[0])
		goto pragma_out
	}
	if rc != int32(SQLITE_NOTFOUND) {
		if (**(**[4]uintptr)(__ccgo_up(bp + 8)))[0] != 0 {
			_sqlite3ErrorMsg(tls, pParse, __ccgo_ts+3944, libc.VaList(bp+176, (**(**[4]uintptr)(__ccgo_up(bp + 8)))[0]))
			Xsqlite3_free(tls, (**(**[4]uintptr)(__ccgo_up(bp + 8)))[0])
		}
		(*TParse)(unsafe.Pointer(pParse)).FnErr = (*TParse)(unsafe.Pointer(pParse)).FnErr + 1
		(*TParse)(unsafe.Pointer(pParse)).Frc = rc
		goto pragma_out
	}
	/* Locate the pragma in the lookup table */
	pPragma = _pragmaLocate(tls, zLeft)
	if pPragma == uintptr(0) {
		/* IMP: R-43042-22504 No error messages are generated if an
		 ** unknown pragma is issued. */
		goto pragma_out
	}
	/* Make sure the database schema is loaded if the pragma requires that */
	if libc.Int32FromUint8((*TPragmaName)(unsafe.Pointer(pPragma)).FmPragFlg)&int32(PragFlg_NeedSchema) != 0 {
		if _sqlite3ReadSchema(tls, pParse) != 0 {
			goto pragma_out
		}
	}
	/* Register the result column names for pragmas that return results */
	if libc.Int32FromUint8((*TPragmaName)(unsafe.Pointer(pPragma)).FmPragFlg)&int32(PragFlg_NoColumns) == 0 && (libc.Int32FromUint8((*TPragmaName)(unsafe.Pointer(pPragma)).FmPragFlg)&int32(PragFlg_NoColumns1) == 0 || zRight == uintptr(0)) {
		_setPragmaResultColumnNames(tls, v, pPragma)
	}
	/* Jump to the appropriate pragma handler */
	switch libc.Int32FromUint8((*TPragmaName)(unsafe.Pointer(pPragma)).FePragTyp) {
	/*
	 **  PRAGMA [schema.]default_cache_size
	 **  PRAGMA [schema.]default_cache_size=N
	 **
	 ** The first form reports the current persistent setting for the
	 ** page cache size.  The value returned is the maximum number of
	 ** pages in the page cache.  The second form sets both the current
	 ** page cache size value and the persistent page cache size value
	 ** stored in the database file.
	 **
	 ** Older versions of SQLite would set the default cache size to a
	 ** negative number to indicate synchronous=OFF.  These days, synchronous
	 ** is always on by default regardless of the sign of the default cache
	 ** size.  But continue to take the absolute value of the default cache
	 ** size of historical compatibility.
	 */
	case int32(PragTyp_DEFAULT_CACHE_SIZE):
		_sqlite3VdbeUsesBtree(tls, v, iDb)
		if !(zRight != 0) {
			**(**int32)(__ccgo_up(pParse + 60)) += int32(2)
			aOp = _sqlite3VdbeAddOpList(tls, v, libc.Int32FromUint64(libc.Uint64FromInt64(36)/libc.Uint64FromInt64(4)), uintptr(unsafe.Pointer(&_getCacheSize)), _iLn3)
			if 0 != 0 {
				break
			}
			(**(**TVdbeOp)(__ccgo_up(aOp))).Fp1 = iDb
			(**(**TVdbeOp)(__ccgo_up(aOp + 1*24))).Fp1 = iDb
			(**(**TVdbeOp)(__ccgo_up(aOp + 6*24))).Fp1 = -int32(2000)
		} else {
			size = _sqlite3AbsInt32(tls, _sqlite3Atoi(tls, zRight))
			_sqlite3BeginWriteOperation(tls, pParse, 0, iDb)
			_sqlite3VdbeAddOp3(tls, v, int32(OP_SetCookie), iDb, int32(BTREE_DEFAULT_CACHE_SIZE), size)
			(*TSchema)(unsafe.Pointer((*TDb)(unsafe.Pointer(pDb)).FpSchema)).Fcache_size = size
			_sqlite3BtreeSetCacheSize(tls, (*TDb)(unsafe.Pointer(pDb)).FpBt, (*TSchema)(unsafe.Pointer((*TDb)(unsafe.Pointer(pDb)).FpSchema)).Fcache_size)
		}
		break
		/*
		 **  PRAGMA [schema.]page_size
		 **  PRAGMA [schema.]page_size=N
		 **
		 ** The first form reports the current setting for the
		 ** database page size in bytes.  The second form sets the
		 ** database page size value.  The value can only be set if
		 ** the database has not yet been created.
		 */
		fallthrough
	case int32(PragTyp_PAGE_SIZE):
		pBt = (*TDb)(unsafe.Pointer(pDb)).FpBt
		if !(zRight != 0) {
			if pBt != 0 {
				v2 = _sqlite3BtreeGetPageSize(tls, pBt)
			} else {
				v2 = 0
			}
			size1 = v2
			_returnSingleInt(tls, v, int64(size1))
		} else {
			/* Malloc may fail when setting the page-size, as there is an internal
			 ** buffer that the pager module resizes using sqlite3_realloc().
			 */
			(*Tsqlite3)(unsafe.Pointer(db)).FnextPagesize = _sqlite3Atoi(tls, zRight)
			if int32(SQLITE_NOMEM) == _sqlite3BtreeSetPageSize(tls, pBt, (*Tsqlite3)(unsafe.Pointer(db)).FnextPagesize, 0, 0) {
				_sqlite3OomFault(tls, db)
			}
		}
		break
		/*
		 **  PRAGMA [schema.]secure_delete
		 **  PRAGMA [schema.]secure_delete=ON/OFF/FAST
		 **
		 ** The first form reports the current setting for the
		 ** secure_delete flag.  The second form changes the secure_delete
		 ** flag setting and reports the new value.
		 */
		fallthrough
	case int32(PragTyp_SECURE_DELETE):
		pBt1 = (*TDb)(unsafe.Pointer(pDb)).FpBt
		b = -int32(1)
		if zRight != 0 {
			if Xsqlite3_stricmp(tls, zRight, __ccgo_ts+19218) == 0 {
				b = int32(2)
			} else {
				b = libc.Int32FromUint8(_sqlite3GetBoolean(tls, zRight, uint8(0)))
			}
		}
		if (*TToken)(unsafe.Pointer(pId2)).Fn == uint32(0) && b >= 0 {
			ii = 0
			for {
				if !(ii < (*Tsqlite3)(unsafe.Pointer(db)).FnDb) {
					break
				}
				_sqlite3BtreeSecureDelete(tls, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii)*32))).FpBt, b)
				goto _3
			_3:
				;
				ii = ii + 1
			}
		}
		b = _sqlite3BtreeSecureDelete(tls, pBt1, b)
		_returnSingleInt(tls, v, int64(b))
		break
		/*
		 **  PRAGMA [schema.]max_page_count
		 **  PRAGMA [schema.]max_page_count=N
		 **
		 ** The first form reports the current setting for the
		 ** maximum number of pages in the database file.  The
		 ** second form attempts to change this setting.  Both
		 ** forms return the current setting.
		 **
		 ** The absolute value of N is used.  This is undocumented and might
		 ** change.  The only purpose is to provide an easy way to test
		 ** the sqlite3AbsInt32() function.
		 **
		 **  PRAGMA [schema.]page_count
		 **
		 ** Return the number of pages in the specified database.
		 */
		fallthrough
	case int32(PragTyp_PAGE_COUNT):
		**(**Ti64)(__ccgo_up(bp + 40)) = 0
		_sqlite3CodeVerifySchema(tls, pParse, iDb)
		v1 = pParse + 60
		*(*int32)(unsafe.Pointer(v1)) = *(*int32)(unsafe.Pointer(v1)) + 1
		v2 = *(*int32)(unsafe.Pointer(v1))
		iReg = v2
		if libc.Int32FromUint8(_sqlite3UpperToLower[uint8(**(**uint8)(__ccgo_up(zLeft)))]) == int32('p') {
			_sqlite3VdbeAddOp2(tls, v, int32(OP_Pagecount), iDb, iReg)
		} else {
			if zRight != 0 && _sqlite3DecOrHexToI64(tls, zRight, bp+40) == 0 {
				if **(**Ti64)(__ccgo_up(bp + 40)) < 0 {
					**(**Ti64)(__ccgo_up(bp + 40)) = 0
				} else {
					if **(**Ti64)(__ccgo_up(bp + 40)) > libc.Int64FromUint32(0xfffffffe) {
						**(**Ti64)(__ccgo_up(bp + 40)) = libc.Int64FromUint32(0xfffffffe)
					}
				}
			} else {
				**(**Ti64)(__ccgo_up(bp + 40)) = 0
			}
			_sqlite3VdbeAddOp3(tls, v, int32(OP_MaxPgcnt), iDb, iReg, int32(**(**Ti64)(__ccgo_up(bp + 40))))
		}
		_sqlite3VdbeAddOp2(tls, v, int32(OP_ResultRow), iReg, int32(1))
		break
		/*
		 **  PRAGMA [schema.]locking_mode
		 **  PRAGMA [schema.]locking_mode = (normal|exclusive)
		 */
		fallthrough
	case int32(PragTyp_LOCKING_MODE):
		zRet = __ccgo_ts + 19009
		eMode = _getLockingMode(tls, zRight)
		if (*TToken)(unsafe.Pointer(pId2)).Fn == uint32(0) && eMode == -int32(1) {
			/* Simple "PRAGMA locking_mode;" statement. This is a query for
			 ** the current default locking mode (which may be different to
			 ** the locking-mode of the main database).
			 */
			eMode = libc.Int32FromUint8((*Tsqlite3)(unsafe.Pointer(db)).FdfltLockMode)
		} else {
			if (*TToken)(unsafe.Pointer(pId2)).Fn == uint32(0) {
				ii1 = int32(2)
				for {
					if !(ii1 < (*Tsqlite3)(unsafe.Pointer(db)).FnDb) {
						break
					}
					pPager = _sqlite3BtreePager(tls, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii1)*32))).FpBt)
					_sqlite3PagerLockingMode(tls, pPager, eMode)
					goto _6
				_6:
					;
					ii1 = ii1 + 1
				}
				(*Tsqlite3)(unsafe.Pointer(db)).FdfltLockMode = libc.Uint8FromInt32(eMode)
			}
			pPager = _sqlite3BtreePager(tls, (*TDb)(unsafe.Pointer(pDb)).FpBt)
			eMode = _sqlite3PagerLockingMode(tls, pPager, eMode)
		}
		if eMode == int32(PAGER_LOCKINGMODE_EXCLUSIVE) {
			zRet = __ccgo_ts + 18999
		}
		_returnSingleText(tls, v, zRet)
		break
		/*
		 **  PRAGMA [schema.]journal_mode
		 **  PRAGMA [schema.]journal_mode =
		 **                      (delete|persist|off|truncate|memory|wal|off)
		 */
		fallthrough
	case int32(PragTyp_JOURNAL_MODE): /* Loop counter */
		if zRight == uintptr(0) {
			/* If there is no "=MODE" part of the pragma, do a query for the
			 ** current mode */
			eMode1 = -int32(1)
		} else {
			n = _sqlite3Strlen30(tls, zRight)
			eMode1 = 0
			for {
				v1 = _sqlite3JournalModename(tls, eMode1)
				zMode = v1
				if !(v1 != uintptr(0)) {
					break
				}
				if Xsqlite3_strnicmp(tls, zRight, zMode, n) == 0 {
					break
				}
				goto _7
			_7:
				;
				eMode1 = eMode1 + 1
			}
			if !(zMode != 0) {
				/* If the "=MODE" part does not match any known journal mode,
				 ** then do a query */
				eMode1 = -int32(1)
			}
			if eMode1 == int32(PAGER_JOURNALMODE_OFF) && (*Tsqlite3)(unsafe.Pointer(db)).Fflags&uint64(SQLITE_Defensive) != uint64(0) {
				/* Do not allow journal-mode "OFF" in defensive since the database
				 ** can become corrupted using ordinary SQL when the journal is off */
				eMode1 = -int32(1)
			}
		}
		if eMode1 == -int32(1) && (*TToken)(unsafe.Pointer(pId2)).Fn == uint32(0) {
			/* Convert "PRAGMA journal_mode" into "PRAGMA main.journal_mode" */
			iDb = 0
			(*TToken)(unsafe.Pointer(pId2)).Fn = uint32(1)
		}
		ii2 = (*Tsqlite3)(unsafe.Pointer(db)).FnDb - int32(1)
		for {
			if !(ii2 >= 0) {
				break
			}
			if (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii2)*32))).FpBt != 0 && (ii2 == iDb || (*TToken)(unsafe.Pointer(pId2)).Fn == uint32(0)) {
				_sqlite3VdbeUsesBtree(tls, v, ii2)
				_sqlite3VdbeAddOp3(tls, v, int32(OP_JournalMode), ii2, int32(1), eMode1)
			}
			goto _9
		_9:
			;
			ii2 = ii2 - 1
		}
		_sqlite3VdbeAddOp2(tls, v, int32(OP_ResultRow), int32(1), int32(1))
		break
		/*
		 **  PRAGMA [schema.]journal_size_limit
		 **  PRAGMA [schema.]journal_size_limit=N
		 **
		 ** Get or set the size limit on rollback journal files.
		 */
		fallthrough
	case int32(PragTyp_JOURNAL_SIZE_LIMIT):
		pPager1 = _sqlite3BtreePager(tls, (*TDb)(unsafe.Pointer(pDb)).FpBt)
		**(**Ti64)(__ccgo_up(bp + 48)) = int64(-int32(2))
		if zRight != 0 {
			_sqlite3DecOrHexToI64(tls, zRight, bp+48)
			if **(**Ti64)(__ccgo_up(bp + 48)) < int64(-int32(1)) {
				**(**Ti64)(__ccgo_up(bp + 48)) = int64(-int32(1))
			}
		}
		**(**Ti64)(__ccgo_up(bp + 48)) = _sqlite3PagerJournalSizeLimit(tls, pPager1, **(**Ti64)(__ccgo_up(bp + 48)))
		_returnSingleInt(tls, v, **(**Ti64)(__ccgo_up(bp + 48)))
		break
		/*
		 **  PRAGMA [schema.]auto_vacuum
		 **  PRAGMA [schema.]auto_vacuum=N
		 **
		 ** Get or set the value of the database 'auto-vacuum' parameter.
		 ** The value is one of:  0 NONE 1 FULL 2 INCREMENTAL
		 */
		fallthrough
	case int32(PragTyp_AUTO_VACUUM):
		pBt2 = (*TDb)(unsafe.Pointer(pDb)).FpBt
		if !(zRight != 0) {
			_returnSingleInt(tls, v, int64(_sqlite3BtreeGetAutoVacuum(tls, pBt2)))
		} else {
			eAuto = _getAutoVacuum(tls, zRight)
			(*Tsqlite3)(unsafe.Pointer(db)).FnextAutovac = libc.Int8FromUint8(libc.Uint8FromInt32(eAuto))
			/* Call SetAutoVacuum() to set initialize the internal auto and
			 ** incr-vacuum flags. This is required in case this connection
			 ** creates the database file. It is important that it is created
			 ** as an auto-vacuum capable db.
			 */
			rc = _sqlite3BtreeSetAutoVacuum(tls, pBt2, eAuto)
			if rc == SQLITE_OK && (eAuto == int32(1) || eAuto == int32(2)) {
				iAddr = _sqlite3VdbeCurrentAddr(tls, v)
				aOp1 = _sqlite3VdbeAddOpList(tls, v, libc.Int32FromUint64(libc.Uint64FromInt64(20)/libc.Uint64FromInt64(4)), uintptr(unsafe.Pointer(&_setMeta6)), _iLn11)
				if 0 != 0 {
					break
				}
				(**(**TVdbeOp)(__ccgo_up(aOp1))).Fp1 = iDb
				(**(**TVdbeOp)(__ccgo_up(aOp1 + 1*24))).Fp1 = iDb
				(**(**TVdbeOp)(__ccgo_up(aOp1 + 2*24))).Fp2 = iAddr + int32(4)
				(**(**TVdbeOp)(__ccgo_up(aOp1 + 4*24))).Fp1 = iDb
				(**(**TVdbeOp)(__ccgo_up(aOp1 + 4*24))).Fp3 = eAuto - int32(1)
				_sqlite3VdbeUsesBtree(tls, v, iDb)
			}
		}
		break
		/*
		 **  PRAGMA [schema.]incremental_vacuum(N)
		 **
		 ** Do N steps of incremental vacuuming on a database.
		 */
		fallthrough
	case int32(PragTyp_INCREMENTAL_VACUUM):
		**(**int32)(__ccgo_up(bp + 56)) = 0
		if zRight == uintptr(0) || !(_sqlite3GetInt32(tls, zRight, bp+56) != 0) || **(**int32)(__ccgo_up(bp + 56)) <= 0 {
			**(**int32)(__ccgo_up(bp + 56)) = int32(0x7fffffff)
		}
		_sqlite3BeginWriteOperation(tls, pParse, 0, iDb)
		_sqlite3VdbeAddOp2(tls, v, int32(OP_Integer), **(**int32)(__ccgo_up(bp + 56)), int32(1))
		addr = _sqlite3VdbeAddOp1(tls, v, int32(OP_IncrVacuum), iDb)
		_sqlite3VdbeAddOp1(tls, v, int32(OP_ResultRow), int32(1))
		_sqlite3VdbeAddOp2(tls, v, int32(OP_AddImm), int32(1), -int32(1))
		_sqlite3VdbeAddOp2(tls, v, int32(OP_IfPos), int32(1), addr)
		_sqlite3VdbeJumpHere(tls, v, addr)
		break
		/*
		 **  PRAGMA [schema.]cache_size
		 **  PRAGMA [schema.]cache_size=N
		 **
		 ** The first form reports the current local setting for the
		 ** page cache size. The second form sets the local
		 ** page cache size value.  If N is positive then that is the
		 ** number of pages in the cache.  If N is negative, then the
		 ** number of pages is adjusted so that the cache uses -N kibibytes
		 ** of memory.
		 */
		fallthrough
	case int32(PragTyp_CACHE_SIZE):
		if !(zRight != 0) {
			_returnSingleInt(tls, v, int64((*TSchema)(unsafe.Pointer((*TDb)(unsafe.Pointer(pDb)).FpSchema)).Fcache_size))
		} else {
			size2 = _sqlite3Atoi(tls, zRight)
			(*TSchema)(unsafe.Pointer((*TDb)(unsafe.Pointer(pDb)).FpSchema)).Fcache_size = size2
			_sqlite3BtreeSetCacheSize(tls, (*TDb)(unsafe.Pointer(pDb)).FpBt, (*TSchema)(unsafe.Pointer((*TDb)(unsafe.Pointer(pDb)).FpSchema)).Fcache_size)
		}
		break
		/*
		 **  PRAGMA [schema.]cache_spill
		 **  PRAGMA cache_spill=BOOLEAN
		 **  PRAGMA [schema.]cache_spill=N
		 **
		 ** The first form reports the current local setting for the
		 ** page cache spill size. The second form turns cache spill on
		 ** or off.  When turning cache spill on, the size is set to the
		 ** current cache_size.  The third form sets a spill size that
		 ** may be different form the cache size.
		 ** If N is positive then that is the
		 ** number of pages in the cache.  If N is negative, then the
		 ** number of pages is adjusted so that the cache uses -N kibibytes
		 ** of memory.
		 **
		 ** If the number of cache_spill pages is less then the number of
		 ** cache_size pages, no spilling occurs until the page count exceeds
		 ** the number of cache_size pages.
		 **
		 ** The cache_spill=BOOLEAN setting applies to all attached schemas,
		 ** not just the schema specified.
		 */
		fallthrough
	case int32(PragTyp_CACHE_SPILL):
		if !(zRight != 0) {
			if (*Tsqlite3)(unsafe.Pointer(db)).Fflags&uint64(SQLITE_CacheSpill) == uint64(0) {
				v2 = 0
			} else {
				v2 = _sqlite3BtreeSetSpillSize(tls, (*TDb)(unsafe.Pointer(pDb)).FpBt, 0)
			}
			_returnSingleInt(tls, v, int64(v2))
		} else {
			**(**int32)(__ccgo_up(bp + 60)) = int32(1)
			if _sqlite3GetInt32(tls, zRight, bp+60) != 0 {
				_sqlite3BtreeSetSpillSize(tls, (*TDb)(unsafe.Pointer(pDb)).FpBt, **(**int32)(__ccgo_up(bp + 60)))
			}
			if _sqlite3GetBoolean(tls, zRight, libc.BoolUint8(**(**int32)(__ccgo_up(bp + 60)) != 0)) != 0 {
				**(**Tu64)(__ccgo_up(db + 48)) |= uint64(SQLITE_CacheSpill)
			} else {
				**(**Tu64)(__ccgo_up(db + 48)) &= ^libc.Uint64FromInt32(SQLITE_CacheSpill)
			}
			_setAllPagerFlags(tls, db)
		}
		break
		/*
		 **  PRAGMA [schema.]mmap_size(N)
		 **
		 ** Used to set mapping size limit. The mapping size limit is
		 ** used to limit the aggregate size of all memory mapped regions of the
		 ** database file. If this parameter is set to zero, then memory mapping
		 ** is not used at all.  If N is negative, then the default memory map
		 ** limit determined by sqlite3_config(SQLITE_CONFIG_MMAP_SIZE) is set.
		 ** The parameter N is measured in bytes.
		 **
		 ** This value is advisory.  The underlying VFS is free to memory map
		 ** as little or as much as it wants.  Except, if N is set to 0 then the
		 ** upper layers will never invoke the xFetch interfaces to the VFS.
		 */
		fallthrough
	case int32(PragTyp_MMAP_SIZE):
		if zRight != 0 {
			_sqlite3DecOrHexToI64(tls, zRight, bp+64)
			if **(**Tsqlite3_int64)(__ccgo_up(bp + 64)) < 0 {
				**(**Tsqlite3_int64)(__ccgo_up(bp + 64)) = _sqlite3Config.FszMmap
			}
			if (*TToken)(unsafe.Pointer(pId2)).Fn == uint32(0) {
				(*Tsqlite3)(unsafe.Pointer(db)).FszMmap = **(**Tsqlite3_int64)(__ccgo_up(bp + 64))
			}
			ii3 = (*Tsqlite3)(unsafe.Pointer(db)).FnDb - int32(1)
			for {
				if !(ii3 >= 0) {
					break
				}
				if (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii3)*32))).FpBt != 0 && (ii3 == iDb || (*TToken)(unsafe.Pointer(pId2)).Fn == uint32(0)) {
					_sqlite3BtreeSetMmapLimit(tls, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii3)*32))).FpBt, **(**Tsqlite3_int64)(__ccgo_up(bp + 64)))
				}
				goto _11
			_11:
				;
				ii3 = ii3 - 1
			}
		}
		**(**Tsqlite3_int64)(__ccgo_up(bp + 64)) = int64(-int32(1))
		rc = Xsqlite3_file_control(tls, db, zDb, int32(SQLITE_FCNTL_MMAP_SIZE), bp+64)
		if rc == SQLITE_OK {
			_returnSingleInt(tls, v, **(**Tsqlite3_int64)(__ccgo_up(bp + 64)))
		} else {
			if rc != int32(SQLITE_NOTFOUND) {
				(*TParse)(unsafe.Pointer(pParse)).FnErr = (*TParse)(unsafe.Pointer(pParse)).FnErr + 1
				(*TParse)(unsafe.Pointer(pParse)).Frc = rc
			}
		}
		break
		/*
		 **   PRAGMA temp_store
		 **   PRAGMA temp_store = "default"|"memory"|"file"
		 **
		 ** Return or set the local value of the temp_store flag.  Changing
		 ** the local value does not make changes to the disk file and the default
		 ** value will be restored the next time the database is opened.
		 **
		 ** Note that it is possible for the library compile-time options to
		 ** override this setting
		 */
		fallthrough
	case int32(PragTyp_TEMP_STORE):
		if !(zRight != 0) {
			_returnSingleInt(tls, v, libc.Int64FromUint8((*Tsqlite3)(unsafe.Pointer(db)).Ftemp_store))
		} else {
			_changeTempStorage(tls, pParse, zRight)
		}
		break
		/*
		 **   PRAGMA temp_store_directory
		 **   PRAGMA temp_store_directory = ""|"directory_name"
		 **
		 ** Return or set the local value of the temp_store_directory flag.  Changing
		 ** the value sets a specific directory to be used for temporary files.
		 ** Setting to a null string reverts to the default temporary directory search.
		 ** If temporary directory is changed, then invalidateTempStorage.
		 **
		 */
		fallthrough
	case int32(PragTyp_TEMP_STORE_DIRECTORY):
		Xsqlite3_mutex_enter(tls, _sqlite3MutexAlloc(tls, int32(SQLITE_MUTEX_STATIC_VFS1)))
		if !(zRight != 0) {
			_returnSingleText(tls, v, Xsqlite3_temp_directory)
		} else {
			if **(**uint8)(__ccgo_up(zRight)) != 0 {
				rc = _sqlite3OsAccess(tls, (*Tsqlite3)(unsafe.Pointer(db)).FpVfs, zRight, int32(SQLITE_ACCESS_READWRITE), bp+72)
				if rc != SQLITE_OK || **(**int32)(__ccgo_up(bp + 72)) == 0 {
					_sqlite3ErrorMsg(tls, pParse, __ccgo_ts+19223, 0)
					Xsqlite3_mutex_leave(tls, _sqlite3MutexAlloc(tls, int32(SQLITE_MUTEX_STATIC_VFS1)))
					goto pragma_out
				}
			}
			if libc.Bool(false) || libc.Bool(true) && libc.Int32FromUint8((*Tsqlite3)(unsafe.Pointer(db)).Ftemp_store) <= int32(1) || libc.Bool(libc.Bool(false) && libc.Int32FromUint8((*Tsqlite3)(unsafe.Pointer(db)).Ftemp_store) == int32(1)) {
				_invalidateTempStorage(tls, pParse)
			}
			Xsqlite3_free(tls, Xsqlite3_temp_directory)
			if **(**uint8)(__ccgo_up(zRight)) != 0 {
				Xsqlite3_temp_directory = Xsqlite3_mprintf(tls, __ccgo_ts+3944, libc.VaList(bp+176, zRight))
			} else {
				Xsqlite3_temp_directory = uintptr(0)
			}
		}
		Xsqlite3_mutex_leave(tls, _sqlite3MutexAlloc(tls, int32(SQLITE_MUTEX_STATIC_VFS1)))
		break
		/*
		 **   PRAGMA [schema.]synchronous
		 **   PRAGMA [schema.]synchronous=OFF|ON|NORMAL|FULL|EXTRA
		 **
		 ** Return or set the local value of the synchronous flag.  Changing
		 ** the local value does not make changes to the disk file and the
		 ** default value will be restored the next time the database is
		 ** opened.
		 */
		fallthrough
	case int32(PragTyp_SYNCHRONOUS):
		if !(zRight != 0) {
			_returnSingleInt(tls, v, int64(libc.Int32FromUint8((*TDb)(unsafe.Pointer(pDb)).Fsafety_level)-int32(1)))
		} else {
			if !((*Tsqlite3)(unsafe.Pointer(db)).FautoCommit != 0) {
				_sqlite3ErrorMsg(tls, pParse, __ccgo_ts+19248, 0)
			} else {
				if iDb != int32(1) {
					iLevel = (libc.Int32FromUint8(_getSafetyLevel(tls, zRight, 0, uint8(1))) + int32(1)) & int32(PAGER_SYNCHRONOUS_MASK)
					if iLevel == 0 {
						iLevel = int32(1)
					}
					(*TDb)(unsafe.Pointer(pDb)).Fsafety_level = libc.Uint8FromInt32(iLevel)
					(*TDb)(unsafe.Pointer(pDb)).FbSyncSet = uint8(1)
					_setAllPagerFlags(tls, db)
				}
			}
		}
	case int32(PragTyp_FLAG):
		if zRight == uintptr(0) {
			_setPragmaResultColumnNames(tls, v, pPragma)
			_returnSingleInt(tls, v, libc.BoolInt64((*Tsqlite3)(unsafe.Pointer(db)).Fflags&(*TPragmaName)(unsafe.Pointer(pPragma)).FiArg != uint64(0)))
		} else {
			mask = (*TPragmaName)(unsafe.Pointer(pPragma)).FiArg /* Mask of bits to set or clear. */
			if libc.Int32FromUint8((*Tsqlite3)(unsafe.Pointer(db)).FautoCommit) == 0 {
				/* Foreign key support may not be enabled or disabled while not
				 ** in auto-commit mode.  */
				mask = mask & libc.Uint64FromInt32(^libc.Int32FromInt32(SQLITE_ForeignKeys))
			}
			if _sqlite3GetBoolean(tls, zRight, uint8(0)) != 0 {
				if mask&uint64(SQLITE_WriteSchema) == uint64(0) || (*Tsqlite3)(unsafe.Pointer(db)).Fflags&uint64(SQLITE_Defensive) == uint64(0) {
					**(**Tu64)(__ccgo_up(db + 48)) |= mask
				}
			} else {
				**(**Tu64)(__ccgo_up(db + 48)) &= ^mask
				if mask == uint64(SQLITE_DeferFKs) {
					(*Tsqlite3)(unsafe.Pointer(db)).FnDeferredImmCons = 0
					(*Tsqlite3)(unsafe.Pointer(db)).FnDeferredCons = 0
				}
				if mask&uint64(SQLITE_WriteSchema) != uint64(0) && Xsqlite3_stricmp(tls, zRight, __ccgo_ts+19301) == 0 {
					/* IMP: R-60817-01178 If the argument is "RESET" then schema
					 ** writing is disabled (as with "PRAGMA writable_schema=OFF") and,
					 ** in addition, the schema is reloaded. */
					_sqlite3ResetAllSchemasOfConnection(tls, db)
				}
			}
			/* Many of the flag-pragmas modify the code generated by the SQL
			 ** compiler (eg. count_changes). So add an opcode to expire all
			 ** compiled SQL statements after modifying a pragma value.
			 */
			_sqlite3VdbeAddOp0(tls, v, int32(OP_Expire))
			_setAllPagerFlags(tls, db)
		}
		break
		/*
		 **   PRAGMA table_info(<table>)
		 **
		 ** Return a single row for each column of the named table. The columns of
		 ** the returned data set are:
		 **
		 ** cid:        Column id (numbered from left to right, starting at 0)
		 ** name:       Column name
		 ** type:       Column declaration type.
		 ** notnull:    True if 'NOT NULL' is part of column declaration
		 ** dflt_value: The default value for the column, if any.
		 ** pk:         Non-zero for PK fields.
		 */
		fallthrough
	case int32(PragTyp_TABLE_INFO):
		if zRight != 0 {
			_sqlite3CodeVerifyNamedSchema(tls, pParse, zDb)
			pTab = _sqlite3LocateTable(tls, pParse, uint32(LOCATE_NOERR), zRight, zDb)
			if pTab != 0 {
				nHidden = 0
				pPk = _sqlite3PrimaryKeyIndex(tls, pTab)
				(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(7)
				_sqlite3ViewGetColumnNames(tls, pParse, pTab)
				i = 0
				pCol = (*TTable)(unsafe.Pointer(pTab)).FaCol
				for {
					if !(i < int32((*TTable)(unsafe.Pointer(pTab)).FnCol)) {
						break
					}
					isHidden = 0
					if libc.Int32FromUint16((*TColumn)(unsafe.Pointer(pCol)).FcolFlags)&int32(COLFLAG_NOINSERT) != 0 {
						if (*TPragmaName)(unsafe.Pointer(pPragma)).FiArg == uint64(0) {
							nHidden = nHidden + 1
							goto _12
						}
						if libc.Int32FromUint16((*TColumn)(unsafe.Pointer(pCol)).FcolFlags)&int32(COLFLAG_VIRTUAL) != 0 {
							isHidden = int32(2) /* GENERATED ALWAYS AS ... VIRTUAL */
						} else {
							if libc.Int32FromUint16((*TColumn)(unsafe.Pointer(pCol)).FcolFlags)&int32(COLFLAG_STORED) != 0 {
								isHidden = int32(3) /* GENERATED ALWAYS AS ... STORED */
							} else {
								isHidden = int32(1) /* HIDDEN */
							}
						}
					}
					if libc.Int32FromUint16((*TColumn)(unsafe.Pointer(pCol)).FcolFlags)&int32(COLFLAG_PRIMKEY) == 0 {
						k = 0
					} else {
						if pPk == uintptr(0) {
							k = int32(1)
						} else {
							k = int32(1)
							for {
								if !(k <= int32((*TTable)(unsafe.Pointer(pTab)).FnCol) && int32(**(**Ti16)(__ccgo_up((*TIndex)(unsafe.Pointer(pPk)).FaiColumn + uintptr(k-int32(1))*2))) != i) {
									break
								}
								goto _13
							_13:
								;
								k = k + 1
							}
						}
					}
					pColExpr = _sqlite3ColumnExpr(tls, pTab, pCol)
					if (*TPragmaName)(unsafe.Pointer(pPragma)).FiArg != 0 {
						v1 = __ccgo_ts + 19307
					} else {
						v1 = __ccgo_ts + 19315
					}
					if int32(uint32(*(*uint8)(unsafe.Pointer(pCol + 8))&0xf>>0)) != 0 {
						v2 = int32(1)
					} else {
						v2 = 0
					}
					if isHidden >= int32(2) || pColExpr == uintptr(0) {
						v5 = uintptr(0)
					} else {
						v5 = *(*uintptr)(unsafe.Pointer(pColExpr + 8))
					}
					_sqlite3VdbeMultiLoad(tls, v, int32(1), v1, libc.VaList(bp+176, i-nHidden, (*TColumn)(unsafe.Pointer(pCol)).FzCnName, _sqlite3ColumnType(tls, pCol, __ccgo_ts+1704), v2, v5, k, isHidden))
					goto _12
				_12:
					;
					i = i + 1
					pCol += 16
				}
			}
		}
		break
		/*
		 **   PRAGMA table_list
		 **
		 ** Return a single row for each table, virtual table, or view in the
		 ** entire schema.
		 **
		 ** schema:     Name of attached database hold this table
		 ** name:       Name of the table itself
		 ** type:       "table", "view", "virtual", "shadow"
		 ** ncol:       Number of columns
		 ** wr:         True for a WITHOUT ROWID table
		 ** strict:     True for a STRICT table
		 */
		fallthrough
	case int32(PragTyp_TABLE_LIST):
		(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(6)
		_sqlite3CodeVerifyNamedSchema(tls, pParse, zDb)
		ii4 = 0
		for {
			if !(ii4 < (*Tsqlite3)(unsafe.Pointer(db)).FnDb) {
				break
			}
			if zDb != 0 && Xsqlite3_stricmp(tls, zDb, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii4)*32))).FzDbSName) != 0 {
				goto _17
			}
			/* Ensure that the Table.nCol field is initialized for all views
			 ** and virtual tables.  Each time we initialize a Table.nCol value
			 ** for a table, that can potentially disrupt the hash table, so restart
			 ** the initialization scan.
			 */
			pHash = (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii4)*32))).FpSchema + 8
			initNCol = libc.Int32FromUint32((*THash)(unsafe.Pointer(pHash)).Fcount)
			for {
				v2 = initNCol
				initNCol = initNCol - 1
				if !(v2 != 0) {
					break
				}
				k1 = (*THash)(unsafe.Pointer(pHash)).Ffirst
				for {
					if !(int32(1) != 0) {
						break
					}
					if k1 == uintptr(0) {
						initNCol = 0
						break
					}
					pTab1 = (*THashElem)(unsafe.Pointer(k1)).Fdata
					if int32((*TTable)(unsafe.Pointer(pTab1)).FnCol) == 0 {
						zSql = _sqlite3MPrintf(tls, db, __ccgo_ts+19322, libc.VaList(bp+176, (*TTable)(unsafe.Pointer(pTab1)).FzName))
						if zSql != 0 {
							**(**uintptr)(__ccgo_up(bp + 80)) = uintptr(0)
							Xsqlite3_prepare_v3(tls, db, zSql, -int32(1), uint32(SQLITE_PREPARE_DONT_LOG), bp+80, uintptr(0))
							Xsqlite3_finalize(tls, **(**uintptr)(__ccgo_up(bp + 80)))
							_sqlite3DbFree(tls, db, zSql)
						}
						if (*Tsqlite3)(unsafe.Pointer(db)).FmallocFailed != 0 {
							_sqlite3ErrorMsg(tls, (*Tsqlite3)(unsafe.Pointer(db)).FpParse, __ccgo_ts+1674, 0)
							(*TParse)(unsafe.Pointer((*Tsqlite3)(unsafe.Pointer(db)).FpParse)).Frc = int32(SQLITE_NOMEM)
						}
						pHash = (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii4)*32))).FpSchema + 8
						break
					}
					goto _19
				_19:
					;
					k1 = (*THashElem)(unsafe.Pointer(k1)).Fnext
				}
			}
			k1 = (*THash)(unsafe.Pointer(pHash)).Ffirst
			for {
				if !(k1 != 0) {
					break
				}
				pTab2 = (*THashElem)(unsafe.Pointer(k1)).Fdata
				if zRight != 0 && Xsqlite3_stricmp(tls, zRight, (*TTable)(unsafe.Pointer(pTab2)).FzName) != 0 {
					goto _20
				}
				if libc.Int32FromUint8((*TTable)(unsafe.Pointer(pTab2)).FeTabType) == int32(TABTYP_VIEW) {
					zType = __ccgo_ts + 11115
				} else {
					if libc.Int32FromUint8((*TTable)(unsafe.Pointer(pTab2)).FeTabType) == int32(TABTYP_VTAB) {
						zType = __ccgo_ts + 14300
					} else {
						if (*TTable)(unsafe.Pointer(pTab2)).FtabFlags&uint32(TF_Shadow) != 0 {
							zType = __ccgo_ts + 19338
						} else {
							zType = __ccgo_ts + 9377
						}
					}
				}
				_sqlite3VdbeMultiLoad(tls, v, int32(1), __ccgo_ts+19345, libc.VaList(bp+176, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(ii4)*32))).FzDbSName, _sqlite3PreferredTableName(tls, (*TTable)(unsafe.Pointer(pTab2)).FzName), zType, int32((*TTable)(unsafe.Pointer(pTab2)).FnCol), libc.BoolInt32((*TTable)(unsafe.Pointer(pTab2)).FtabFlags&uint32(TF_WithoutRowid) != uint32(0)), libc.BoolInt32((*TTable)(unsafe.Pointer(pTab2)).FtabFlags&uint32(TF_Strict) != uint32(0))))
				goto _20
			_20:
				;
				k1 = (*THashElem)(unsafe.Pointer(k1)).Fnext
			}
			goto _17
		_17:
			;
			ii4 = ii4 + 1
		}
	case int32(PragTyp_INDEX_INFO):
		if zRight != 0 {
			pIdx = _sqlite3FindIndex(tls, db, zRight, zDb)
			if pIdx == uintptr(0) {
				/* If there is no index named zRight, check to see if there is a
				 ** WITHOUT ROWID table named zRight, and if there is, show the
				 ** structure of the PRIMARY KEY index for that table. */
				pTab3 = _sqlite3LocateTable(tls, pParse, uint32(LOCATE_NOERR), zRight, zDb)
				if pTab3 != 0 && !((*TTable)(unsafe.Pointer(pTab3)).FtabFlags&libc.Uint32FromInt32(TF_WithoutRowid) == libc.Uint32FromInt32(0)) {
					pIdx = _sqlite3PrimaryKeyIndex(tls, pTab3)
				}
			}
			if pIdx != 0 {
				iIdxDb = _sqlite3SchemaToIndex(tls, db, (*TIndex)(unsafe.Pointer(pIdx)).FpSchema)
				if (*TPragmaName)(unsafe.Pointer(pPragma)).FiArg != 0 {
					/* PRAGMA index_xinfo (newer version with more rows and columns) */
					mx = libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pIdx)).FnColumn)
					(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(6)
				} else {
					/* PRAGMA index_info (legacy version) */
					mx = libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pIdx)).FnKeyCol)
					(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(3)
				}
				pTab3 = (*TIndex)(unsafe.Pointer(pIdx)).FpTable
				_sqlite3CodeVerifySchema(tls, pParse, iIdxDb)
				i1 = 0
				for {
					if !(i1 < mx) {
						break
					}
					cnum = **(**Ti16)(__ccgo_up((*TIndex)(unsafe.Pointer(pIdx)).FaiColumn + uintptr(i1)*2))
					if int32(cnum) < 0 {
						v1 = uintptr(0)
					} else {
						v1 = (**(**TColumn)(__ccgo_up((*TTable)(unsafe.Pointer(pTab3)).FaCol + uintptr(cnum)*16))).FzCnName
					}
					_sqlite3VdbeMultiLoad(tls, v, int32(1), __ccgo_ts+19352, libc.VaList(bp+176, i1, int32(cnum), v1))
					if (*TPragmaName)(unsafe.Pointer(pPragma)).FiArg != 0 {
						_sqlite3VdbeMultiLoad(tls, v, int32(4), __ccgo_ts+19357, libc.VaList(bp+176, libc.Int32FromUint8(**(**Tu8)(__ccgo_up((*TIndex)(unsafe.Pointer(pIdx)).FaSortOrder + uintptr(i1)))), **(**uintptr)(__ccgo_up((*TIndex)(unsafe.Pointer(pIdx)).FazColl + uintptr(i1)*8)), libc.BoolInt32(i1 < libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pIdx)).FnKeyCol))))
					}
					_sqlite3VdbeAddOp2(tls, v, int32(OP_ResultRow), int32(1), (*TParse)(unsafe.Pointer(pParse)).FnMem)
					goto _21
				_21:
					;
					i1 = i1 + 1
				}
			}
		}
	case int32(PragTyp_INDEX_LIST):
		if zRight != 0 {
			pTab4 = _sqlite3FindTable(tls, db, zRight, zDb)
			if pTab4 != 0 {
				iTabDb = _sqlite3SchemaToIndex(tls, db, (*TTable)(unsafe.Pointer(pTab4)).FpSchema)
				(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(5)
				_sqlite3CodeVerifySchema(tls, pParse, iTabDb)
				pIdx1 = (*TTable)(unsafe.Pointer(pTab4)).FpIndex
				i2 = libc.Int32FromInt32(0)
				for {
					if !(pIdx1 != 0) {
						break
					}
					azOrigin = [3]uintptr{
						0: __ccgo_ts + 19362,
						1: __ccgo_ts + 19364,
						2: __ccgo_ts + 17851,
					}
					_sqlite3VdbeMultiLoad(tls, v, int32(1), __ccgo_ts+19366, libc.VaList(bp+176, i2, (*TIndex)(unsafe.Pointer(pIdx1)).FzName, libc.BoolInt32(libc.Int32FromUint8((*TIndex)(unsafe.Pointer(pIdx1)).FonError) != OE_None), azOrigin[int32(uint32(*(*uint16)(unsafe.Pointer(pIdx1 + 100))&0x3>>0))], libc.BoolInt32((*TIndex)(unsafe.Pointer(pIdx1)).FpPartIdxWhere != uintptr(0))))
					goto _23
				_23:
					;
					pIdx1 = (*TIndex)(unsafe.Pointer(pIdx1)).FpNext
					i2 = i2 + 1
				}
			}
		}
	case int32(PragTyp_DATABASE_LIST):
		(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(3)
		i3 = 0
		for {
			if !(i3 < (*Tsqlite3)(unsafe.Pointer(db)).FnDb) {
				break
			}
			if (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(i3)*32))).FpBt == uintptr(0) {
				goto _24
			}
			_sqlite3VdbeMultiLoad(tls, v, int32(1), __ccgo_ts+19372, libc.VaList(bp+176, i3, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(i3)*32))).FzDbSName, _sqlite3BtreeGetFilename(tls, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(i3)*32))).FpBt)))
			goto _24
		_24:
			;
			i3 = i3 + 1
		}
	case int32(PragTyp_COLLATION_LIST):
		i4 = 0
		(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(2)
		p = (*THash)(unsafe.Pointer(db + 648)).Ffirst
		for {
			if !(p != 0) {
				break
			}
			pColl = (*THashElem)(unsafe.Pointer(p)).Fdata
			v2 = i4
			i4 = i4 + 1
			_sqlite3VdbeMultiLoad(tls, v, int32(1), __ccgo_ts+19376, libc.VaList(bp+176, v2, (*TCollSeq)(unsafe.Pointer(pColl)).FzName))
			goto _25
		_25:
			;
			p = (*THashElem)(unsafe.Pointer(p)).Fnext
		}
	case int32(PragTyp_FUNCTION_LIST):
		showInternFunc = libc.BoolInt32((*Tsqlite3)(unsafe.Pointer(db)).FmDbFlags&uint32(DBFLAG_InternalFunc) != uint32(0))
		(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(6)
		i5 = 0
		for {
			if !(i5 < int32(SQLITE_FUNC_HASH_SZ)) {
				break
			}
			p1 = **(**uintptr)(__ccgo_up(uintptr(unsafe.Pointer(&_sqlite3BuiltinFunctions)) + uintptr(i5)*8))
			for {
				if !(p1 != 0) {
					break
				}
				_pragmaFunclistLine(tls, v, p1, int32(1), showInternFunc)
				goto _28
			_28:
				;
				p1 = *(*uintptr)(unsafe.Pointer(p1 + 64))
			}
			goto _27
		_27:
			;
			i5 = i5 + 1
		}
		j = (*THash)(unsafe.Pointer(db + 624)).Ffirst
		for {
			if !(j != 0) {
				break
			}
			p1 = (*THashElem)(unsafe.Pointer(j)).Fdata
			_pragmaFunclistLine(tls, v, p1, 0, showInternFunc)
			goto _29
		_29:
			;
			j = (*THashElem)(unsafe.Pointer(j)).Fnext
		}
	case int32(PragTyp_MODULE_LIST):
		(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(1)
		j1 = (*THash)(unsafe.Pointer(db + 576)).Ffirst
		for {
			if !(j1 != 0) {
				break
			}
			pMod = (*THashElem)(unsafe.Pointer(j1)).Fdata
			_sqlite3VdbeMultiLoad(tls, v, int32(1), __ccgo_ts+7909, libc.VaList(bp+176, (*TModule)(unsafe.Pointer(pMod)).FzName))
			goto _30
		_30:
			;
			j1 = (*THashElem)(unsafe.Pointer(j1)).Fnext
		}
	case int32(PragTyp_PRAGMA_LIST):
		i6 = 0
		for {
			if !(i6 < libc.Int32FromUint64(libc.Uint64FromInt64(1584)/libc.Uint64FromInt64(24))) {
				break
			}
			_sqlite3VdbeMultiLoad(tls, v, int32(1), __ccgo_ts+7909, libc.VaList(bp+176, _aPragmaName[i6].FzName))
			goto _31
		_31:
			;
			i6 = i6 + 1
		}
	case int32(PragTyp_FOREIGN_KEY_LIST):
		if zRight != 0 {
			pTab5 = _sqlite3FindTable(tls, db, zRight, zDb)
			if pTab5 != 0 && libc.Int32FromUint8((*TTable)(unsafe.Pointer(pTab5)).FeTabType) == TABTYP_NORM {
				pFK = (*(*struct {
					FaddColOffset int32
					FpFKey        uintptr
					FpDfltList    uintptr
				})(unsafe.Pointer(pTab5 + 64))).FpFKey
				if pFK != 0 {
					iTabDb1 = _sqlite3SchemaToIndex(tls, db, (*TTable)(unsafe.Pointer(pTab5)).FpSchema)
					i7 = 0
					(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(8)
					_sqlite3CodeVerifySchema(tls, pParse, iTabDb1)
					for pFK != 0 {
						j2 = 0
						for {
							if !(j2 < (*TFKey)(unsafe.Pointer(pFK)).FnCol) {
								break
							}
							_sqlite3VdbeMultiLoad(tls, v, int32(1), __ccgo_ts+19379, libc.VaList(bp+176, i7, j2, (*TFKey)(unsafe.Pointer(pFK)).FzTo, (**(**TColumn)(__ccgo_up((*TTable)(unsafe.Pointer(pTab5)).FaCol + uintptr((*(*TsColMap)(unsafe.Pointer(pFK + 64 + uintptr(j2)*16))).FiFrom)*16))).FzCnName, (*(*TsColMap)(unsafe.Pointer(pFK + 64 + uintptr(j2)*16))).FzCol, _actionName(tls, **(**Tu8)(__ccgo_up(pFK + 45 + 1))), _actionName(tls, **(**Tu8)(__ccgo_up(pFK + 45))), __ccgo_ts+19388))
							goto _32
						_32:
							;
							j2 = j2 + 1
						}
						i7 = i7 + 1
						pFK = (*TFKey)(unsafe.Pointer(pFK)).FpNextFrom
					}
				}
			}
		}
	case int32(PragTyp_FOREIGN_KEY_CHECK): /* child to parent column mapping */
		regResult = (*TParse)(unsafe.Pointer(pParse)).FnMem + int32(1)
		**(**int32)(__ccgo_up(pParse + 60)) += int32(4)
		v1 = pParse + 60
		*(*int32)(unsafe.Pointer(v1)) = *(*int32)(unsafe.Pointer(v1)) + 1
		v2 = *(*int32)(unsafe.Pointer(v1))
		regRow = v2
		k2 = (*THash)(unsafe.Pointer((**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(iDb)*32))).FpSchema + 8)).Ffirst
		for k2 != 0 {
			if zRight != 0 {
				pTab6 = _sqlite3LocateTable(tls, pParse, uint32(0), zRight, zDb)
				k2 = uintptr(0)
			} else {
				pTab6 = (*THashElem)(unsafe.Pointer(k2)).Fdata
				k2 = (*THashElem)(unsafe.Pointer(k2)).Fnext
			}
			if pTab6 == uintptr(0) || !(libc.Int32FromUint8((*TTable)(unsafe.Pointer(pTab6)).FeTabType) == libc.Int32FromInt32(TABTYP_NORM)) || (*(*struct {
				FaddColOffset int32
				FpFKey        uintptr
				FpDfltList    uintptr
			})(unsafe.Pointer(pTab6 + 64))).FpFKey == uintptr(0) {
				continue
			}
			iDb = _sqlite3SchemaToIndex(tls, db, (*TTable)(unsafe.Pointer(pTab6)).FpSchema)
			zDb = (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(iDb)*32))).FzDbSName
			_sqlite3CodeVerifySchema(tls, pParse, iDb)
			_sqlite3TableLock(tls, pParse, iDb, (*TTable)(unsafe.Pointer(pTab6)).Ftnum, uint8(0), (*TTable)(unsafe.Pointer(pTab6)).FzName)
			_sqlite3TouchRegister(tls, pParse, int32((*TTable)(unsafe.Pointer(pTab6)).FnCol)+regRow)
			_sqlite3OpenTable(tls, pParse, 0, iDb, pTab6, int32(OP_OpenRead))
			_sqlite3VdbeLoadString(tls, v, regResult, (*TTable)(unsafe.Pointer(pTab6)).FzName)
			i8 = int32(1)
			pFK1 = (*(*struct {
				FaddColOffset int32
				FpFKey        uintptr
				FpDfltList    uintptr
			})(unsafe.Pointer(pTab6 + 64))).FpFKey
			for {
				if !(pFK1 != 0) {
					break
				}
				pParent = _sqlite3FindTable(tls, db, (*TFKey)(unsafe.Pointer(pFK1)).FzTo, zDb)
				if pParent == uintptr(0) {
					goto _35
				}
				**(**uintptr)(__ccgo_up(bp + 88)) = uintptr(0)
				_sqlite3TableLock(tls, pParse, iDb, (*TTable)(unsafe.Pointer(pParent)).Ftnum, uint8(0), (*TTable)(unsafe.Pointer(pParent)).FzName)
				x1 = _sqlite3FkLocateIndex(tls, pParse, pParent, pFK1, bp+88, uintptr(0))
				if x1 == 0 {
					if **(**uintptr)(__ccgo_up(bp + 88)) == uintptr(0) {
						_sqlite3OpenTable(tls, pParse, i8, iDb, pParent, int32(OP_OpenRead))
					} else {
						_sqlite3VdbeAddOp3(tls, v, int32(OP_OpenRead), i8, libc.Int32FromUint32((*TIndex)(unsafe.Pointer(**(**uintptr)(__ccgo_up(bp + 88)))).Ftnum), iDb)
						_sqlite3VdbeSetP4KeyInfo(tls, pParse, **(**uintptr)(__ccgo_up(bp + 88)))
					}
				} else {
					k2 = uintptr(0)
					break
				}
				goto _35
			_35:
				;
				i8 = i8 + 1
				pFK1 = (*TFKey)(unsafe.Pointer(pFK1)).FpNextFrom
			}
			if pFK1 != 0 {
				break
			}
			if (*TParse)(unsafe.Pointer(pParse)).FnTab < i8 {
				(*TParse)(unsafe.Pointer(pParse)).FnTab = i8
			}
			addrTop = _sqlite3VdbeAddOp1(tls, v, int32(OP_Rewind), 0)
			i8 = int32(1)
			pFK1 = (*(*struct {
				FaddColOffset int32
				FpFKey        uintptr
				FpDfltList    uintptr
			})(unsafe.Pointer(pTab6 + 64))).FpFKey
			for {
				if !(pFK1 != 0) {
					break
				}
				pParent = _sqlite3FindTable(tls, db, (*TFKey)(unsafe.Pointer(pFK1)).FzTo, zDb)
				**(**uintptr)(__ccgo_up(bp + 88)) = uintptr(0)
				**(**uintptr)(__ccgo_up(bp + 96)) = uintptr(0)
				if pParent != 0 {
					x1 = _sqlite3FkLocateIndex(tls, pParse, pParent, pFK1, bp+88, bp+96)
				}
				addrOk = _sqlite3VdbeMakeLabel(tls, pParse)
				/* Generate code to read the child key values into registers
				 ** regRow..regRow+n. If any of the child key values are NULL, this
				 ** row cannot cause an FK violation. Jump directly to addrOk in
				 ** this case. */
				_sqlite3TouchRegister(tls, pParse, regRow+(*TFKey)(unsafe.Pointer(pFK1)).FnCol)
				j3 = 0
				for {
					if !(j3 < (*TFKey)(unsafe.Pointer(pFK1)).FnCol) {
						break
					}
					if **(**uintptr)(__ccgo_up(bp + 96)) != 0 {
						v2 = **(**int32)(__ccgo_up(**(**uintptr)(__ccgo_up(bp + 96)) + uintptr(j3)*4))
					} else {
						v2 = (*(*TsColMap)(unsafe.Pointer(pFK1 + 64 + uintptr(j3)*16))).FiFrom
					}
					iCol = v2
					_sqlite3ExprCodeGetColumnOfTable(tls, v, pTab6, 0, iCol, regRow+j3)
					_sqlite3VdbeAddOp2(tls, v, int32(OP_IsNull), regRow+j3, addrOk)
					goto _37
				_37:
					;
					j3 = j3 + 1
				}
				/* Generate code to query the parent index for a matching parent
				 ** key. If a match is found, jump to addrOk. */
				if **(**uintptr)(__ccgo_up(bp + 88)) != 0 {
					_sqlite3VdbeAddOp4(tls, v, int32(OP_Affinity), regRow, (*TFKey)(unsafe.Pointer(pFK1)).FnCol, 0, _sqlite3IndexAffinityStr(tls, db, **(**uintptr)(__ccgo_up(bp + 88))), (*TFKey)(unsafe.Pointer(pFK1)).FnCol)
					_sqlite3VdbeAddOp4Int(tls, v, int32(OP_Found), i8, addrOk, regRow, (*TFKey)(unsafe.Pointer(pFK1)).FnCol)
				} else {
					if pParent != 0 {
						jmp = _sqlite3VdbeCurrentAddr(tls, v) + int32(2)
						_sqlite3VdbeAddOp3(tls, v, int32(OP_SeekRowid), i8, jmp, regRow)
						_sqlite3VdbeGoto(tls, v, addrOk)
					}
				}
				/* Generate code to report an FK violation to the caller. */
				if (*TTable)(unsafe.Pointer(pTab6)).FtabFlags&uint32(TF_WithoutRowid) == uint32(0) {
					_sqlite3VdbeAddOp2(tls, v, int32(OP_Rowid), 0, regResult+int32(1))
				} else {
					_sqlite3VdbeAddOp2(tls, v, int32(OP_Null), 0, regResult+int32(1))
				}
				_sqlite3VdbeMultiLoad(tls, v, regResult+int32(2), __ccgo_ts+19393, libc.VaList(bp+176, (*TFKey)(unsafe.Pointer(pFK1)).FzTo, i8-int32(1)))
				_sqlite3VdbeAddOp2(tls, v, int32(OP_ResultRow), regResult, int32(4))
				_sqlite3VdbeResolveLabel(tls, v, addrOk)
				_sqlite3DbFree(tls, db, **(**uintptr)(__ccgo_up(bp + 96)))
				goto _36
			_36:
				;
				i8 = i8 + 1
				pFK1 = (*TFKey)(unsafe.Pointer(pFK1)).FpNextFrom
			}
			_sqlite3VdbeAddOp2(tls, v, int32(OP_Next), 0, addrTop+int32(1))
			_sqlite3VdbeJumpHere(tls, v, addrTop)
		}
		break
		/* Reinstall the LIKE and GLOB functions.  The variant of LIKE
		 ** used will be case sensitive or not depending on the RHS.
		 */
		fallthrough
	case int32(PragTyp_CASE_SENSITIVE_LIKE):
		if zRight != 0 {
			_sqlite3RegisterLikeFunctions(tls, db, libc.Int32FromUint8(_sqlite3GetBoolean(tls, zRight, uint8(0))))
		}
		break
		/*    PRAGMA integrity_check
		 **    PRAGMA integrity_check(N)
		 **    PRAGMA quick_check
		 **    PRAGMA quick_check(N)
		 **
		 ** Verify the integrity of the database.
		 **
		 ** The "quick_check" is reduced version of
		 ** integrity_check designed to detect most database corruption
		 ** without the overhead of cross-checking indexes.  Quick_check
		 ** is linear time whereas integrity_check is O(NlogN).
		 **
		 ** The maximum number of errors is 100 by default.  A different default
		 ** can be specified using a numeric parameter N.
		 **
		 ** Or, the parameter N can be the name of a table.  In that case, only
		 ** the one table named is verified.  The freelist is only verified if
		 ** the named table is "sqlite_schema" (or one of its aliases).
		 **
		 ** All schemas are checked by default.  To check just a single
		 ** schema, use the form:
		 **
		 **      PRAGMA schema.integrity_check;
		 */
		fallthrough
	case int32(PragTyp_INTEGRITY_CHECK):
		pObjTab = uintptr(0) /* Check only this one table, if not NULL */
		isQuick = libc.BoolInt32(libc.Int32FromUint8(_sqlite3UpperToLower[uint8(**(**uint8)(__ccgo_up(zLeft)))]) == int32('q'))
		/* If the PRAGMA command was of the form "PRAGMA <db>.integrity_check",
		 ** then iDb is set to the index of the database identified by <db>.
		 ** In this case, the integrity of database iDb only is verified by
		 ** the VDBE created below.
		 **
		 ** Otherwise, if the command was simply "PRAGMA integrity_check" (or
		 ** "PRAGMA quick_check"), then iDb is set to 0. In this case, set iDb
		 ** to -1 here, to indicate that the VDBE should verify the integrity
		 ** of all attached databases.  */
		if (*TToken)(unsafe.Pointer(pId2)).Fz == uintptr(0) {
			iDb = -int32(1)
		}
		/* Initialize the VDBE program */
		(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(6)
		/* Set the maximum error count */
		**(**int32)(__ccgo_up(bp + 104)) = int32(SQLITE_INTEGRITY_CHECK_ERROR_MAX)
		if zRight != 0 {
			if _sqlite3GetInt32(tls, (*TToken)(unsafe.Pointer(pValue)).Fz, bp+104) != 0 {
				if **(**int32)(__ccgo_up(bp + 104)) <= 0 {
					**(**int32)(__ccgo_up(bp + 104)) = int32(SQLITE_INTEGRITY_CHECK_ERROR_MAX)
				}
			} else {
				if iDb >= 0 {
					v1 = (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(iDb)*32))).FzDbSName
				} else {
					v1 = uintptr(0)
				}
				pObjTab = _sqlite3LocateTable(tls, pParse, uint32(0), zRight, v1)
			}
		}
		_sqlite3VdbeAddOp2(tls, v, int32(OP_Integer), **(**int32)(__ccgo_up(bp + 104))-int32(1), int32(1)) /* reg[1] holds errors left */
		/* Do an integrity check on each database file */
		i9 = 0
		for {
			if !(i9 < (*Tsqlite3)(unsafe.Pointer(db)).FnDb) {
				break
			} /* Array of root page numbers of all btrees */
			cnt = 0 /* Number of entries in aRoot[] */
			if libc.Bool(OMIT_TEMPDB != 0) && i9 == int32(1) {
				goto _40
			}
			if iDb >= 0 && i9 != iDb {
				goto _40
			}
			_sqlite3CodeVerifySchema(tls, pParse, i9)
			libc.SetBitFieldPtr16Uint32(pParse+40, libc.Uint32FromInt32(0), 7, 0x80) /* tag-20230327-1 */
			/* Do an integrity check of the B-Tree
			 **
			 ** Begin by finding the root pages numbers
			 ** for all tables and indices in the database.
			 */
			pTbls = (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(i9)*32))).FpSchema + 8
			cnt = 0
			x2 = (*THash)(unsafe.Pointer(pTbls)).Ffirst
			for {
				if !(x2 != 0) {
					break
				}
				pTab7 = (*THashElem)(unsafe.Pointer(x2)).Fdata /* Number of indexes on pTab */
				if _tableSkipIntegrityCheck(tls, pTab7, pObjTab) != 0 {
					goto _41
				}
				if (*TTable)(unsafe.Pointer(pTab7)).FtabFlags&uint32(TF_WithoutRowid) == uint32(0) {
					cnt = cnt + 1
				}
				nIdx = 0
				pIdx3 = (*TTable)(unsafe.Pointer(pTab7)).FpIndex
				for {
					if !(pIdx3 != 0) {
						break
					}
					cnt = cnt + 1
					goto _42
				_42:
					;
					pIdx3 = (*TIndex)(unsafe.Pointer(pIdx3)).FpNext
					nIdx = nIdx + 1
				}
				goto _41
			_41:
				;
				x2 = (*THashElem)(unsafe.Pointer(x2)).Fnext
			}
			if cnt == 0 {
				goto _40
			}
			if pObjTab != 0 {
				cnt = cnt + 1
			}
			aRoot = _sqlite3DbMallocRawNN(tls, db, uint64(uint64(4)*libc.Uint64FromInt32(cnt+libc.Int32FromInt32(1))))
			if aRoot == uintptr(0) {
				break
			}
			cnt = 0
			if pObjTab != 0 {
				cnt = cnt + 1
				v2 = cnt
				**(**int32)(__ccgo_up(aRoot + uintptr(v2)*4)) = 0
			}
			x2 = (*THash)(unsafe.Pointer(pTbls)).Ffirst
			for {
				if !(x2 != 0) {
					break
				}
				pTab8 = (*THashElem)(unsafe.Pointer(x2)).Fdata
				if _tableSkipIntegrityCheck(tls, pTab8, pObjTab) != 0 {
					goto _44
				}
				if (*TTable)(unsafe.Pointer(pTab8)).FtabFlags&uint32(TF_WithoutRowid) == uint32(0) {
					cnt = cnt + 1
					v2 = cnt
					**(**int32)(__ccgo_up(aRoot + uintptr(v2)*4)) = libc.Int32FromUint32((*TTable)(unsafe.Pointer(pTab8)).Ftnum)
				}
				pIdx4 = (*TTable)(unsafe.Pointer(pTab8)).FpIndex
				for {
					if !(pIdx4 != 0) {
						break
					}
					cnt = cnt + 1
					v2 = cnt
					**(**int32)(__ccgo_up(aRoot + uintptr(v2)*4)) = libc.Int32FromUint32((*TIndex)(unsafe.Pointer(pIdx4)).Ftnum)
					goto _46
				_46:
					;
					pIdx4 = (*TIndex)(unsafe.Pointer(pIdx4)).FpNext
				}
				goto _44
			_44:
				;
				x2 = (*THashElem)(unsafe.Pointer(x2)).Fnext
			}
			**(**int32)(__ccgo_up(aRoot)) = cnt
			/* Make sure sufficient number of registers have been allocated */
			_sqlite3TouchRegister(tls, pParse, int32(8)+cnt)
			_sqlite3VdbeAddOp3(tls, v, int32(OP_Null), 0, int32(8), int32(8)+cnt)
			_sqlite3ClearTempRegCache(tls, pParse)
			/* Do the b-tree integrity checks */
			_sqlite3VdbeAddOp4(tls, v, int32(OP_IntegrityCk), int32(1), cnt, int32(8), aRoot, -int32(15))
			_sqlite3VdbeChangeP5(tls, v, libc.Uint16FromInt32(i9))
			addr1 = _sqlite3VdbeAddOp1(tls, v, int32(OP_IsNull), int32(2))
			_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, int32(3), 0, _sqlite3MPrintf(tls, db, __ccgo_ts+19397, libc.VaList(bp+176, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(i9)*32))).FzDbSName)), -int32(7))
			_sqlite3VdbeAddOp3(tls, v, int32(OP_Concat), int32(2), int32(3), int32(3))
			_integrityCheckResultRow(tls, v)
			_sqlite3VdbeJumpHere(tls, v, addr1)
			/* Check that the indexes all have the right number of rows */
			if pObjTab != 0 {
				v2 = int32(1)
			} else {
				v2 = 0
			}
			cnt = v2
			_sqlite3VdbeLoadString(tls, v, int32(2), __ccgo_ts+19421)
			x2 = (*THash)(unsafe.Pointer(pTbls)).Ffirst
			for {
				if !(x2 != 0) {
					break
				}
				iTab = 0
				pTab9 = (*THashElem)(unsafe.Pointer(x2)).Fdata
				if _tableSkipIntegrityCheck(tls, pTab9, pObjTab) != 0 {
					goto _49
				}
				if (*TTable)(unsafe.Pointer(pTab9)).FtabFlags&uint32(TF_WithoutRowid) == uint32(0) {
					v2 = cnt
					cnt = cnt + 1
					iTab = v2
				} else {
					iTab = cnt
					pIdx5 = (*TTable)(unsafe.Pointer(pTab9)).FpIndex
					for {
						if !(pIdx5 != 0) {
							break
						}
						if int32(uint32(*(*uint16)(unsafe.Pointer(pIdx5 + 100))&0x3>>0)) == int32(SQLITE_IDXTYPE_PRIMARYKEY) {
							break
						}
						iTab = iTab + 1
						goto _51
					_51:
						;
						pIdx5 = (*TIndex)(unsafe.Pointer(pIdx5)).FpNext
					}
				}
				pIdx5 = (*TTable)(unsafe.Pointer(pTab9)).FpIndex
				for {
					if !(pIdx5 != 0) {
						break
					}
					if (*TIndex)(unsafe.Pointer(pIdx5)).FpPartIdxWhere == uintptr(0) {
						addr1 = _sqlite3VdbeAddOp3(tls, v, int32(OP_Eq), int32(8)+cnt, 0, int32(8)+iTab)
						_sqlite3VdbeLoadString(tls, v, int32(4), (*TIndex)(unsafe.Pointer(pIdx5)).FzName)
						_sqlite3VdbeAddOp3(tls, v, int32(OP_Concat), int32(4), int32(2), int32(3))
						_integrityCheckResultRow(tls, v)
						_sqlite3VdbeJumpHere(tls, v, addr1)
					}
					cnt = cnt + 1
					goto _52
				_52:
					;
					pIdx5 = (*TIndex)(unsafe.Pointer(pIdx5)).FpNext
				}
				goto _49
			_49:
				;
				x2 = (*THashElem)(unsafe.Pointer(x2)).Fnext
			}
			/* Make sure all the indices are constructed correctly.
			 */
			x2 = (*THash)(unsafe.Pointer(pTbls)).Ffirst
			for {
				if !(x2 != 0) {
					break
				}
				pTab10 = (*THashElem)(unsafe.Pointer(x2)).Fdata
				pPrior = uintptr(0)
				r1 = -int32(1) /* Maximum non-virtual column number */
				if _tableSkipIntegrityCheck(tls, pTab10, pObjTab) != 0 {
					goto _53
				}
				if !(libc.Int32FromUint8((*TTable)(unsafe.Pointer(pTab10)).FeTabType) == libc.Int32FromInt32(TABTYP_NORM)) {
					goto _53
				}
				if isQuick != 0 || (*TTable)(unsafe.Pointer(pTab10)).FtabFlags&uint32(TF_WithoutRowid) == uint32(0) {
					pPk1 = uintptr(0)
					r2 = 0
				} else {
					pPk1 = _sqlite3PrimaryKeyIndex(tls, pTab10)
					r2 = _sqlite3GetTempRange(tls, pParse, libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pPk1)).FnKeyCol))
					_sqlite3VdbeAddOp3(tls, v, int32(OP_Null), int32(1), r2, r2+libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pPk1)).FnKeyCol)-int32(1))
				}
				_sqlite3OpenTableAndIndices(tls, pParse, pTab10, int32(OP_OpenRead), uint8(0), int32(1), uintptr(0), bp+108, bp+112)
				/* reg[7] counts the number of entries in the table.
				 ** reg[8+i] counts the number of entries in the i-th index
				 */
				_sqlite3VdbeAddOp2(tls, v, int32(OP_Integer), 0, int32(7))
				j4 = 0
				pIdx6 = (*TTable)(unsafe.Pointer(pTab10)).FpIndex
				for {
					if !(pIdx6 != 0) {
						break
					}
					_sqlite3VdbeAddOp2(tls, v, int32(OP_Integer), 0, int32(8)+j4) /* index entries counter */
					goto _54
				_54:
					;
					pIdx6 = (*TIndex)(unsafe.Pointer(pIdx6)).FpNext
					j4 = j4 + 1
				}
				_sqlite3VdbeAddOp2(tls, v, int32(OP_Rewind), **(**int32)(__ccgo_up(bp + 108)), 0)
				loopTop = _sqlite3VdbeAddOp2(tls, v, int32(OP_AddImm), int32(7), int32(1))
				/* Fetch the right-most column from the table.  This will cause
				 ** the entire record header to be parsed and sanity checked.  It
				 ** will also prepopulate the cursor column cache that is used
				 ** by the OP_IsType code, so it is a required step.
				 */
				if (*TTable)(unsafe.Pointer(pTab10)).FtabFlags&uint32(TF_WithoutRowid) == uint32(0) {
					mxCol = -int32(1)
					j4 = 0
					for {
						if !(j4 < int32((*TTable)(unsafe.Pointer(pTab10)).FnCol)) {
							break
						}
						if libc.Int32FromUint16((**(**TColumn)(__ccgo_up((*TTable)(unsafe.Pointer(pTab10)).FaCol + uintptr(j4)*16))).FcolFlags)&int32(COLFLAG_VIRTUAL) == 0 {
							mxCol = mxCol + 1
						}
						goto _55
					_55:
						;
						j4 = j4 + 1
					}
					if mxCol == int32((*TTable)(unsafe.Pointer(pTab10)).FiPKey) {
						mxCol = mxCol - 1
					}
				} else {
					/* COLFLAG_VIRTUAL columns are not included in the WITHOUT ROWID
					 ** PK index column-count, so there is no need to account for them
					 ** in this case. */
					mxCol = libc.Int32FromUint16((*TIndex)(unsafe.Pointer(_sqlite3PrimaryKeyIndex(tls, pTab10))).FnColumn) - int32(1)
				}
				if mxCol >= 0 {
					_sqlite3VdbeAddOp3(tls, v, int32(OP_Column), **(**int32)(__ccgo_up(bp + 108)), mxCol, int32(3))
					_sqlite3VdbeTypeofColumn(tls, v, int32(3))
				}
				if !(isQuick != 0) {
					if pPk1 != 0 {
						a1 = _sqlite3VdbeAddOp4Int(tls, v, int32(OP_IdxGT), **(**int32)(__ccgo_up(bp + 108)), 0, r2, libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pPk1)).FnKeyCol))
						_sqlite3VdbeAddOp1(tls, v, int32(OP_IsNull), r2)
						zErr = _sqlite3MPrintf(tls, db, __ccgo_ts+19450, libc.VaList(bp+176, (*TTable)(unsafe.Pointer(pTab10)).FzName))
						_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, int32(3), 0, zErr, -int32(7))
						_integrityCheckResultRow(tls, v)
						_sqlite3VdbeJumpHere(tls, v, a1)
						_sqlite3VdbeJumpHere(tls, v, a1+int32(1))
						j4 = 0
						for {
							if !(j4 < libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pPk1)).FnKeyCol)) {
								break
							}
							_sqlite3ExprCodeLoadIndexColumn(tls, pParse, pPk1, **(**int32)(__ccgo_up(bp + 108)), j4, r2+j4)
							goto _56
						_56:
							;
							j4 = j4 + 1
						}
					}
				}
				/* Verify datatypes for all columns:
				 **
				 **   (1) NOT NULL columns may not contain a NULL
				 **   (2) Datatype must be exact for non-ANY columns in STRICT tables
				 **   (3) Datatype for TEXT columns in non-STRICT tables must be
				 **       NULL, TEXT, or BLOB.
				 **   (4) Datatype for numeric columns in non-STRICT tables must not
				 **       be a TEXT value that can be losslessly converted to numeric.
				 */
				bStrict = libc.BoolInt32((*TTable)(unsafe.Pointer(pTab10)).FtabFlags&uint32(TF_Strict) != uint32(0))
				j4 = 0
				for {
					if !(j4 < int32((*TTable)(unsafe.Pointer(pTab10)).FnCol)) {
						break
					}
					pCol1 = (*TTable)(unsafe.Pointer(pTab10)).FaCol + uintptr(j4)*16 /* Check datatypes (besides NOT NULL) */
					if j4 == int32((*TTable)(unsafe.Pointer(pTab10)).FiPKey) {
						goto _57
					}
					if bStrict != 0 {
						doTypeCheck = libc.BoolInt32(int32(uint32(*(*uint8)(unsafe.Pointer(pCol1 + 8))&0xf0>>4)) > int32(COLTYPE_ANY))
					} else {
						doTypeCheck = libc.BoolInt32(libc.Int32FromUint8((*TColumn)(unsafe.Pointer(pCol1)).Faffinity) > int32(SQLITE_AFF_BLOB))
					}
					if int32(uint32(*(*uint8)(unsafe.Pointer(pCol1 + 8))&0xf>>0)) == 0 && !(doTypeCheck != 0) {
						goto _57
					}
					/* Compute the operands that will be needed for OP_IsType */
					p4 = int32(SQLITE_NULL)
					if libc.Int32FromUint16((*TColumn)(unsafe.Pointer(pCol1)).FcolFlags)&int32(COLFLAG_VIRTUAL) != 0 {
						_sqlite3ExprCodeGetColumnOfTable(tls, v, pTab10, **(**int32)(__ccgo_up(bp + 108)), j4, int32(3))
						p11 = -int32(1)
						p3 = int32(3)
					} else {
						if (*TColumn)(unsafe.Pointer(pCol1)).FiDflt != 0 {
							**(**uintptr)(__ccgo_up(bp + 120)) = uintptr(0)
							_sqlite3ValueFromExpr(tls, db, _sqlite3ColumnExpr(tls, pTab10, pCol1), (*Tsqlite3)(unsafe.Pointer(db)).Fenc, (*TColumn)(unsafe.Pointer(pCol1)).Faffinity, bp+120)
							if **(**uintptr)(__ccgo_up(bp + 120)) != 0 {
								p4 = Xsqlite3_value_type(tls, **(**uintptr)(__ccgo_up(bp + 120)))
								_sqlite3ValueFree(tls, **(**uintptr)(__ccgo_up(bp + 120)))
							}
						}
						p11 = **(**int32)(__ccgo_up(bp + 108))
						if !((*TTable)(unsafe.Pointer(pTab10)).FtabFlags&libc.Uint32FromInt32(TF_WithoutRowid) == libc.Uint32FromInt32(0)) {
							p3 = _sqlite3TableColumnToIndex(tls, _sqlite3PrimaryKeyIndex(tls, pTab10), j4)
						} else {
							p3 = int32(_sqlite3TableColumnToStorage(tls, pTab10, int16(j4)))
						}
					}
					labelError = _sqlite3VdbeMakeLabel(tls, pParse)
					labelOk = _sqlite3VdbeMakeLabel(tls, pParse)
					if int32(uint32(*(*uint8)(unsafe.Pointer(pCol1 + 8))&0xf>>0)) != 0 {
						jmp2 = _sqlite3VdbeAddOp4Int(tls, v, int32(OP_IsType), p11, labelOk, p3, p4)
						if p11 < 0 {
							_sqlite3VdbeChangeP5(tls, v, uint16(0x0f)) /* INT, REAL, TEXT, or BLOB */
							jmp3 = jmp2
						} else {
							_sqlite3VdbeChangeP5(tls, v, uint16(0x0d)) /* INT, TEXT, or BLOB */
							/* OP_IsType does not detect NaN values in the database file
							 ** which should be treated as a NULL.  So if the header type
							 ** is REAL, we have to load the actual data using OP_Column
							 ** to reliably determine if the value is a NULL. */
							_sqlite3VdbeAddOp3(tls, v, int32(OP_Column), p11, p3, int32(3))
							_sqlite3ColumnDefault(tls, v, pTab10, j4, int32(3))
							jmp3 = _sqlite3VdbeAddOp2(tls, v, int32(OP_NotNull), int32(3), labelOk)
						}
						zErr1 = _sqlite3MPrintf(tls, db, __ccgo_ts+19486, libc.VaList(bp+176, (*TTable)(unsafe.Pointer(pTab10)).FzName, (*TColumn)(unsafe.Pointer(pCol1)).FzCnName))
						_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, int32(3), 0, zErr1, -int32(7))
						if doTypeCheck != 0 {
							_sqlite3VdbeGoto(tls, v, labelError)
							_sqlite3VdbeJumpHere(tls, v, jmp2)
							_sqlite3VdbeJumpHere(tls, v, jmp3)
						} else {
							/* VDBE byte code will fall thru */
						}
					}
					if bStrict != 0 && doTypeCheck != 0 {
						_sqlite3VdbeAddOp4Int(tls, v, int32(OP_IsType), p11, labelOk, p3, p4)
						_sqlite3VdbeChangeP5(tls, v, uint16(_aStdTypeMask[int32(uint32(*(*uint8)(unsafe.Pointer(pCol1 + 8))&0xf0>>4))-int32(1)]))
						zErr1 = _sqlite3MPrintf(tls, db, __ccgo_ts+19506, libc.VaList(bp+176, _sqlite3StdType[int32(uint32(*(*uint8)(unsafe.Pointer(pCol1 + 8))&0xf0>>4))-int32(1)], (*TTable)(unsafe.Pointer(pTab10)).FzName, (**(**TColumn)(__ccgo_up((*TTable)(unsafe.Pointer(pTab10)).FaCol + uintptr(j4)*16))).FzCnName))
						_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, int32(3), 0, zErr1, -int32(7))
					} else {
						if !(bStrict != 0) && libc.Int32FromUint8((*TColumn)(unsafe.Pointer(pCol1)).Faffinity) == int32(SQLITE_AFF_TEXT) {
							/* (3) Datatype for TEXT columns in non-STRICT tables must be
							 **     NULL, TEXT, or BLOB. */
							_sqlite3VdbeAddOp4Int(tls, v, int32(OP_IsType), p11, labelOk, p3, p4)
							_sqlite3VdbeChangeP5(tls, v, uint16(0x1c)) /* NULL, TEXT, or BLOB */
							zErr1 = _sqlite3MPrintf(tls, db, __ccgo_ts+19528, libc.VaList(bp+176, (*TTable)(unsafe.Pointer(pTab10)).FzName, (**(**TColumn)(__ccgo_up((*TTable)(unsafe.Pointer(pTab10)).FaCol + uintptr(j4)*16))).FzCnName))
							_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, int32(3), 0, zErr1, -int32(7))
						} else {
							if !(bStrict != 0) && libc.Int32FromUint8((*TColumn)(unsafe.Pointer(pCol1)).Faffinity) >= int32(SQLITE_AFF_NUMERIC) {
								/* (4) Datatype for numeric columns in non-STRICT tables must not
								 **     be a TEXT value that can be converted to numeric. */
								_sqlite3VdbeAddOp4Int(tls, v, int32(OP_IsType), p11, labelOk, p3, p4)
								_sqlite3VdbeChangeP5(tls, v, uint16(0x1b)) /* NULL, INT, FLOAT, or BLOB */
								if p11 >= 0 {
									_sqlite3ExprCodeGetColumnOfTable(tls, v, pTab10, **(**int32)(__ccgo_up(bp + 108)), j4, int32(3))
								}
								_sqlite3VdbeAddOp4(tls, v, int32(OP_Affinity), int32(3), int32(1), 0, __ccgo_ts+19551, -int32(1))
								_sqlite3VdbeAddOp4Int(tls, v, int32(OP_IsType), -int32(1), labelOk, int32(3), p4)
								_sqlite3VdbeChangeP5(tls, v, uint16(0x1c)) /* NULL, TEXT, or BLOB */
								zErr1 = _sqlite3MPrintf(tls, db, __ccgo_ts+19553, libc.VaList(bp+176, (*TTable)(unsafe.Pointer(pTab10)).FzName, (**(**TColumn)(__ccgo_up((*TTable)(unsafe.Pointer(pTab10)).FaCol + uintptr(j4)*16))).FzCnName))
								_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, int32(3), 0, zErr1, -int32(7))
							}
						}
					}
					_sqlite3VdbeResolveLabel(tls, v, labelError)
					_integrityCheckResultRow(tls, v)
					_sqlite3VdbeResolveLabel(tls, v, labelOk)
					goto _57
				_57:
					;
					j4 = j4 + 1
				}
				/* Verify CHECK constraints */
				if (*TTable)(unsafe.Pointer(pTab10)).FpCheck != 0 && (*Tsqlite3)(unsafe.Pointer(db)).Fflags&uint64(SQLITE_IgnoreChecks) == uint64(0) {
					pCheck = _sqlite3ExprListDup(tls, db, (*TTable)(unsafe.Pointer(pTab10)).FpCheck, 0)
					if libc.Int32FromUint8((*Tsqlite3)(unsafe.Pointer(db)).FmallocFailed) == 0 {
						addrCkFault = _sqlite3VdbeMakeLabel(tls, pParse)
						addrCkOk = _sqlite3VdbeMakeLabel(tls, pParse)
						(*TParse)(unsafe.Pointer(pParse)).FiSelfTab = **(**int32)(__ccgo_up(bp + 108)) + int32(1)
						k3 = (*TExprList)(unsafe.Pointer(pCheck)).FnExpr - int32(1)
						for {
							if !(k3 > 0) {
								break
							}
							_sqlite3ExprIfFalse(tls, pParse, (*(*TExprList_item)(unsafe.Pointer(pCheck + 8 + uintptr(k3)*32))).FpExpr, addrCkFault, 0)
							goto _58
						_58:
							;
							k3 = k3 - 1
						}
						_sqlite3ExprIfTrue(tls, pParse, (*(*TExprList_item)(unsafe.Pointer(pCheck + 8))).FpExpr, addrCkOk, int32(SQLITE_JUMPIFNULL))
						_sqlite3VdbeResolveLabel(tls, v, addrCkFault)
						(*TParse)(unsafe.Pointer(pParse)).FiSelfTab = 0
						zErr2 = _sqlite3MPrintf(tls, db, __ccgo_ts+19573, libc.VaList(bp+176, (*TTable)(unsafe.Pointer(pTab10)).FzName))
						_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, int32(3), 0, zErr2, -int32(7))
						_integrityCheckResultRow(tls, v)
						_sqlite3VdbeResolveLabel(tls, v, addrCkOk)
					}
					_sqlite3ExprListDelete(tls, db, pCheck)
				}
				if !(isQuick != 0) { /* Omit the remaining tests for quick_check */
					/* Validate index entries for the current row */
					j4 = 0
					pIdx6 = (*TTable)(unsafe.Pointer(pTab10)).FpIndex
					for {
						if !(pIdx6 != 0) {
							break
						}
						ckUniq = _sqlite3VdbeMakeLabel(tls, pParse)
						if pPk1 == pIdx6 {
							goto _59
						}
						r1 = _sqlite3GenerateIndexKey(tls, pParse, pIdx6, **(**int32)(__ccgo_up(bp + 108)), 0, 0, bp+128, pPrior, r1)
						pPrior = pIdx6
						_sqlite3VdbeAddOp2(tls, v, int32(OP_AddImm), int32(8)+j4, int32(1)) /* increment entry count */
						/* Verify that an index entry exists for the current table row */
						_sqlite3VdbeAddOp4Int(tls, v, int32(OP_Found), **(**int32)(__ccgo_up(bp + 112))+j4, ckUniq, r1, libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pIdx6)).FnColumn))
						jmp21 = _sqlite3VdbeAddOp3(tls, v, int32(OP_IFindKey), **(**int32)(__ccgo_up(bp + 112))+j4, ckUniq, r1)
						_sqlite3VdbeChangeP4(tls, v, -int32(1), pIdx6, -int32(6))
						_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, int32(3), 0, _sqlite3MPrintf(tls, db, __ccgo_ts+19603, libc.VaList(bp+176, (*TIndex)(unsafe.Pointer(pIdx6)).FzName)), -int32(7))
						_sqlite3VdbeAddOp3(tls, v, int32(OP_Concat), int32(7), int32(3), int32(3))
						_integrityCheckResultRow(tls, v)
						_sqlite3VdbeAddOp2(tls, v, int32(OP_Goto), 0, ckUniq)
						_sqlite3VdbeJumpHere(tls, v, jmp21)
						_sqlite3VdbeLoadString(tls, v, int32(3), __ccgo_ts+19662)
						_sqlite3VdbeAddOp3(tls, v, int32(OP_Concat), int32(7), int32(3), int32(3))
						_sqlite3VdbeLoadString(tls, v, int32(4), __ccgo_ts+19667)
						_sqlite3VdbeAddOp3(tls, v, int32(OP_Concat), int32(4), int32(3), int32(3))
						jmp5 = _sqlite3VdbeLoadString(tls, v, int32(4), (*TIndex)(unsafe.Pointer(pIdx6)).FzName)
						_sqlite3VdbeAddOp3(tls, v, int32(OP_Concat), int32(4), int32(3), int32(3))
						jmp4 = _integrityCheckResultRow(tls, v)
						_sqlite3VdbeResolveLabel(tls, v, ckUniq)
						/* The OP_IdxRowid opcode is an optimized version of OP_Column
						 ** that extracts the rowid off the end of the index record.
						 ** But it only works correctly if index record does not have
						 ** any extra bytes at the end.  Verify that this is the case. */
						if (*TTable)(unsafe.Pointer(pTab10)).FtabFlags&uint32(TF_WithoutRowid) == uint32(0) {
							_sqlite3VdbeAddOp2(tls, v, int32(OP_IdxRowid), **(**int32)(__ccgo_up(bp + 112))+j4, int32(3))
							jmp7 = _sqlite3VdbeAddOp3(tls, v, int32(OP_Eq), int32(3), 0, r1+libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pIdx6)).FnColumn)-int32(1))
							_sqlite3VdbeLoadString(tls, v, int32(3), __ccgo_ts+19688)
							_sqlite3VdbeAddOp3(tls, v, int32(OP_Concat), int32(7), int32(3), int32(3))
							_sqlite3VdbeLoadString(tls, v, int32(4), __ccgo_ts+19724)
							_sqlite3VdbeGoto(tls, v, jmp5-int32(1))
							_sqlite3VdbeJumpHere(tls, v, jmp7)
						}
						/* Any indexed columns with non-BINARY collations must still hold
						 ** the exact same text value as the table. */
						label6 = 0
						kk = 0
						for {
							if !(kk < libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pIdx6)).FnKeyCol)) {
								break
							}
							if **(**uintptr)(__ccgo_up((*TIndex)(unsafe.Pointer(pIdx6)).FazColl + uintptr(kk)*8)) == uintptr(unsafe.Pointer(&_sqlite3StrBINARY)) {
								goto _60
							}
							if label6 == 0 {
								label6 = _sqlite3VdbeMakeLabel(tls, pParse)
							}
							_sqlite3VdbeAddOp3(tls, v, int32(OP_Column), **(**int32)(__ccgo_up(bp + 112))+j4, kk, int32(3))
							_sqlite3VdbeAddOp3(tls, v, int32(OP_Ne), int32(3), label6, r1+kk)
							goto _60
						_60:
							;
							kk = kk + 1
						}
						if label6 != 0 {
							jmp6 = _sqlite3VdbeAddOp0(tls, v, int32(OP_Goto))
							_sqlite3VdbeResolveLabel(tls, v, label6)
							_sqlite3VdbeLoadString(tls, v, int32(3), __ccgo_ts+19662)
							_sqlite3VdbeAddOp3(tls, v, int32(OP_Concat), int32(7), int32(3), int32(3))
							_sqlite3VdbeLoadString(tls, v, int32(4), __ccgo_ts+19735)
							_sqlite3VdbeGoto(tls, v, jmp5-int32(1))
							_sqlite3VdbeJumpHere(tls, v, jmp6)
						}
						/* For UNIQUE indexes, verify that only one entry exists with the
						 ** current key.  The entry is unique if (1) any column is NULL
						 ** or (2) the next entry has a different key */
						if libc.Int32FromUint8((*TIndex)(unsafe.Pointer(pIdx6)).FonError) != OE_None {
							uniqOk = _sqlite3VdbeMakeLabel(tls, pParse)
							kk = 0
							for {
								if !(kk < libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pIdx6)).FnKeyCol)) {
									break
								}
								iCol1 = int32(**(**Ti16)(__ccgo_up((*TIndex)(unsafe.Pointer(pIdx6)).FaiColumn + uintptr(kk)*2)))
								if iCol1 >= 0 && int32(uint32(*(*uint8)(unsafe.Pointer((*TTable)(unsafe.Pointer(pTab10)).FaCol + uintptr(iCol1)*16 + 8))&0xf>>0)) != 0 {
									goto _61
								}
								_sqlite3VdbeAddOp2(tls, v, int32(OP_IsNull), r1+kk, uniqOk)
								goto _61
							_61:
								;
								kk = kk + 1
							}
							jmp61 = _sqlite3VdbeAddOp1(tls, v, int32(OP_Next), **(**int32)(__ccgo_up(bp + 112))+j4)
							_sqlite3VdbeGoto(tls, v, uniqOk)
							_sqlite3VdbeJumpHere(tls, v, jmp61)
							_sqlite3VdbeAddOp4Int(tls, v, int32(OP_IdxGT), **(**int32)(__ccgo_up(bp + 112))+j4, uniqOk, r1, libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pIdx6)).FnKeyCol))
							_sqlite3VdbeLoadString(tls, v, int32(3), __ccgo_ts+19762)
							_sqlite3VdbeGoto(tls, v, jmp5)
							_sqlite3VdbeResolveLabel(tls, v, uniqOk)
						}
						_sqlite3VdbeJumpHere(tls, v, jmp4)
						_sqlite3ResolvePartIdxLabel(tls, pParse, **(**int32)(__ccgo_up(bp + 128)))
						goto _59
					_59:
						;
						pIdx6 = (*TIndex)(unsafe.Pointer(pIdx6)).FpNext
						j4 = j4 + 1
					}
				}
				_sqlite3VdbeAddOp2(tls, v, int32(OP_Next), **(**int32)(__ccgo_up(bp + 108)), loopTop)
				_sqlite3VdbeJumpHere(tls, v, loopTop-int32(1))
				if pPk1 != 0 {
					_sqlite3ReleaseTempRange(tls, pParse, r2, libc.Int32FromUint16((*TIndex)(unsafe.Pointer(pPk1)).FnKeyCol))
				}
				goto _53
			_53:
				;
				x2 = (*THashElem)(unsafe.Pointer(x2)).Fnext
			}
			/* Second pass to invoke the xIntegrity method on all virtual
			 ** tables.
			 */
			x2 = (*THash)(unsafe.Pointer(pTbls)).Ffirst
			for {
				if !(x2 != 0) {
					break
				}
				pTab11 = (*THashElem)(unsafe.Pointer(x2)).Fdata
				if _tableSkipIntegrityCheck(tls, pTab11, pObjTab) != 0 {
					goto _62
				}
				if libc.Int32FromUint8((*TTable)(unsafe.Pointer(pTab11)).FeTabType) == TABTYP_NORM {
					goto _62
				}
				if !(libc.Int32FromUint8((*TTable)(unsafe.Pointer(pTab11)).FeTabType) == libc.Int32FromInt32(TABTYP_VTAB)) {
					goto _62
				}
				if int32((*TTable)(unsafe.Pointer(pTab11)).FnCol) <= 0 {
					zMod = **(**uintptr)(__ccgo_up((*(*struct {
						FnArg  int32
						FazArg uintptr
						Fp     uintptr
					})(unsafe.Pointer(pTab11 + 64))).FazArg))
					if _sqlite3HashFind(tls, db+576, zMod) == uintptr(0) {
						goto _62
					}
				}
				_sqlite3ViewGetColumnNames(tls, pParse, pTab11)
				if (*(*struct {
					FnArg  int32
					FazArg uintptr
					Fp     uintptr
				})(unsafe.Pointer(pTab11 + 64))).Fp == uintptr(0) {
					goto _62
				}
				pVTab = (*TVTable)(unsafe.Pointer((*(*struct {
					FnArg  int32
					FazArg uintptr
					Fp     uintptr
				})(unsafe.Pointer(pTab11 + 64))).Fp)).FpVtab
				if pVTab == uintptr(0) {
					goto _62
				}
				if (*Tsqlite3_vtab)(unsafe.Pointer(pVTab)).FpModule == uintptr(0) {
					goto _62
				}
				if (*Tsqlite3_module)(unsafe.Pointer((*Tsqlite3_vtab)(unsafe.Pointer(pVTab)).FpModule)).FiVersion < int32(4) {
					goto _62
				}
				if (*Tsqlite3_module)(unsafe.Pointer((*Tsqlite3_vtab)(unsafe.Pointer(pVTab)).FpModule)).FxIntegrity == uintptr(0) {
					goto _62
				}
				_sqlite3VdbeAddOp3(tls, v, int32(OP_VCheck), i9, int32(3), isQuick)
				(*TTable)(unsafe.Pointer(pTab11)).FnTabRef = (*TTable)(unsafe.Pointer(pTab11)).FnTabRef + 1
				_sqlite3VdbeAppendP4(tls, v, pTab11, -int32(17))
				a11 = _sqlite3VdbeAddOp1(tls, v, int32(OP_IsNull), int32(3))
				_integrityCheckResultRow(tls, v)
				_sqlite3VdbeJumpHere(tls, v, a11)
				goto _62
				goto _62
			_62:
				;
				x2 = (*THashElem)(unsafe.Pointer(x2)).Fnext
			}
			goto _40
		_40:
			;
			i9 = i9 + 1
		}
		aOp2 = _sqlite3VdbeAddOpList(tls, v, libc.Int32FromUint64(libc.Uint64FromInt64(28)/libc.Uint64FromInt64(4)), uintptr(unsafe.Pointer(&_endCode)), _iLn21)
		if aOp2 != 0 {
			(**(**TVdbeOp)(__ccgo_up(aOp2))).Fp2 = int32(1) - **(**int32)(__ccgo_up(bp + 104))
			(**(**TVdbeOp)(__ccgo_up(aOp2 + 2*24))).Fp4type = int8(-libc.Int32FromInt32(1))
			*(*uintptr)(unsafe.Pointer(aOp2 + 2*24 + 16)) = __ccgo_ts + 19789
			(**(**TVdbeOp)(__ccgo_up(aOp2 + 5*24))).Fp4type = int8(-libc.Int32FromInt32(1))
			*(*uintptr)(unsafe.Pointer(aOp2 + 5*24 + 16)) = _sqlite3ErrStr(tls, int32(SQLITE_CORRUPT))
		}
		_sqlite3VdbeChangeP3(tls, v, 0, _sqlite3VdbeCurrentAddr(tls, v)-int32(2))
		break
		/*
		 **   PRAGMA encoding
		 **   PRAGMA encoding = "utf-8"|"utf-16"|"utf-16le"|"utf-16be"
		 **
		 ** In its first form, this pragma returns the encoding of the main
		 ** database. If the database is not initialized, it is initialized now.
		 **
		 ** The second form of this pragma is a no-op if the main database file
		 ** has not already been initialized. In this case it sets the default
		 ** encoding that will be used for the main database file if a new file
		 ** is created. If an existing main database file is opened, then the
		 ** default text encoding for the existing database is used.
		 **
		 ** In all cases new databases created using the ATTACH command are
		 ** created to use the same default text encoding as the main database. If
		 ** the main database has not been initialized and/or created when ATTACH
		 ** is executed, this is done before the ATTACH operation.
		 **
		 ** In the second form this pragma sets the text encoding to be used in
		 ** new database files created using this database handle. It is only
		 ** useful if invoked immediately after the main database i
		 */
		fallthrough
	case int32(PragTyp_ENCODING):
		if !(zRight != 0) { /* "PRAGMA encoding" */
			if _sqlite3ReadSchema(tls, pParse) != 0 {
				goto pragma_out
			}
			_returnSingleText(tls, v, _encnames1[(*Tsqlite3)(unsafe.Pointer((*TParse)(unsafe.Pointer(pParse)).Fdb)).Fenc].FzName)
		} else { /* "PRAGMA encoding = XXX" */
			/* Only change the value of sqlite.enc if the database handle is not
			 ** initialized. If the main database exists, the new sqlite.enc value
			 ** will be overwritten when the schema is next loaded. If it does not
			 ** already exists, it will be created to use the new encoding value.
			 */
			if (*Tsqlite3)(unsafe.Pointer(db)).FmDbFlags&uint32(DBFLAG_EncodingFixed) == uint32(0) {
				pEnc = uintptr(unsafe.Pointer(&_encnames1))
				for {
					if !((*struct {
						FzName uintptr
						Fenc   Tu8
					})(unsafe.Pointer(pEnc)).FzName != 0) {
						break
					}
					if 0 == _sqlite3StrICmp(tls, zRight, (*struct {
						FzName uintptr
						Fenc   Tu8
					})(unsafe.Pointer(pEnc)).FzName) {
						if (*struct {
							FzName uintptr
							Fenc   Tu8
						})(unsafe.Pointer(pEnc)).Fenc != 0 {
							v2 = libc.Int32FromUint8((*struct {
								FzName uintptr
								Fenc   Tu8
							})(unsafe.Pointer(pEnc)).Fenc)
						} else {
							v2 = int32(SQLITE_UTF16BE)
						}
						enc = libc.Uint8FromInt32(v2)
						(*TSchema)(unsafe.Pointer((**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb))).FpSchema)).Fenc = enc
						_sqlite3SetTextEncoding(tls, db, enc)
						break
					}
					goto _63
				_63:
					;
					pEnc += 16
				}
				if !((*struct {
					FzName uintptr
					Fenc   Tu8
				})(unsafe.Pointer(pEnc)).FzName != 0) {
					_sqlite3ErrorMsg(tls, pParse, __ccgo_ts+19850, libc.VaList(bp+176, zRight))
				}
			}
		}
		break
		/*
		 **   PRAGMA [schema.]schema_version
		 **   PRAGMA [schema.]schema_version = <integer>
		 **
		 **   PRAGMA [schema.]user_version
		 **   PRAGMA [schema.]user_version = <integer>
		 **
		 **   PRAGMA [schema.]freelist_count
		 **
		 **   PRAGMA [schema.]data_version
		 **
		 **   PRAGMA [schema.]application_id
		 **   PRAGMA [schema.]application_id = <integer>
		 **
		 ** The pragma's schema_version and user_version are used to set or get
		 ** the value of the schema-version and user-version, respectively. Both
		 ** the schema-version and the user-version are 32-bit signed integers
		 ** stored in the database header.
		 **
		 ** The schema-cookie is usually only manipulated internally by SQLite. It
		 ** is incremented by SQLite whenever the database schema is modified (by
		 ** creating or dropping a table or index). The schema version is used by
		 ** SQLite each time a query is executed to ensure that the internal cache
		 ** of the schema used when compiling the SQL query matches the schema of
		 ** the database against which the compiled query is actually executed.
		 ** Subverting this mechanism by using "PRAGMA schema_version" to modify
		 ** the schema-version is potentially dangerous and may lead to program
		 ** crashes or database corruption. Use with caution!
		 **
		 ** The user-version is not used internally by SQLite. It may be used by
		 ** applications for any purpose.
		 */
		fallthrough
	case int32(PragTyp_HEADER_VALUE):
		iCookie = libc.Int32FromUint64((*TPragmaName)(unsafe.Pointer(pPragma)).FiArg) /* Which cookie to read or write */
		_sqlite3VdbeUsesBtree(tls, v, iDb)
		if zRight != 0 && libc.Int32FromUint8((*TPragmaName)(unsafe.Pointer(pPragma)).FmPragFlg)&int32(PragFlg_ReadOnly) == 0 {
			aOp3 = _sqlite3VdbeAddOpList(tls, v, libc.Int32FromUint64(libc.Uint64FromInt64(8)/libc.Uint64FromInt64(4)), uintptr(unsafe.Pointer(&_setCookie)), 0)
			if 0 != 0 {
				break
			}
			(**(**TVdbeOp)(__ccgo_up(aOp3))).Fp1 = iDb
			(**(**TVdbeOp)(__ccgo_up(aOp3 + 1*24))).Fp1 = iDb
			(**(**TVdbeOp)(__ccgo_up(aOp3 + 1*24))).Fp2 = iCookie
			(**(**TVdbeOp)(__ccgo_up(aOp3 + 1*24))).Fp3 = _sqlite3Atoi(tls, zRight)
			(**(**TVdbeOp)(__ccgo_up(aOp3 + 1*24))).Fp5 = uint16(1)
			if iCookie == int32(BTREE_SCHEMA_VERSION) && (*Tsqlite3)(unsafe.Pointer(db)).Fflags&uint64(SQLITE_Defensive) != uint64(0) {
				/* Do not allow the use of PRAGMA schema_version=VALUE in defensive
				 ** mode.  Change the OP_SetCookie opcode into a no-op.  */
				(**(**TVdbeOp)(__ccgo_up(aOp3 + 1*24))).Fopcode = uint8(OP_Noop)
			}
		} else {
			aOp4 = _sqlite3VdbeAddOpList(tls, v, libc.Int32FromUint64(libc.Uint64FromInt64(12)/libc.Uint64FromInt64(4)), uintptr(unsafe.Pointer(&_readCookie)), 0)
			if 0 != 0 {
				break
			}
			(**(**TVdbeOp)(__ccgo_up(aOp4))).Fp1 = iDb
			(**(**TVdbeOp)(__ccgo_up(aOp4 + 1*24))).Fp1 = iDb
			(**(**TVdbeOp)(__ccgo_up(aOp4 + 1*24))).Fp3 = iCookie
			_sqlite3VdbeReusable(tls, v)
		}
		break
		/*
		 **   PRAGMA compile_options
		 **
		 ** Return the names of all compile-time options used in this build,
		 ** one option per row.
		 */
		fallthrough
	case int32(PragTyp_COMPILE_OPTIONS):
		i10 = 0
		(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(1)
		for {
			v2 = i10
			i10 = i10 + 1
			v1 = Xsqlite3_compileoption_get(tls, v2)
			zOpt = v1
			if !(v1 != uintptr(0)) {
				break
			}
			_sqlite3VdbeLoadString(tls, v, int32(1), zOpt)
			_sqlite3VdbeAddOp2(tls, v, int32(OP_ResultRow), int32(1), int32(1))
		}
		_sqlite3VdbeReusable(tls, v)
		break
		/*
		 **   PRAGMA [schema.]wal_checkpoint = passive|full|restart|truncate
		 **
		 ** Checkpoint the database.
		 */
		fallthrough
	case int32(PragTyp_WAL_CHECKPOINT):
		if (*TToken)(unsafe.Pointer(pId2)).Fz != 0 {
			v2 = iDb
		} else {
			v2 = libc.Int32FromInt32(SQLITE_MAX_ATTACHED) + libc.Int32FromInt32(2)
		}
		iBt = v2
		eMode2 = SQLITE_CHECKPOINT_PASSIVE
		if zRight != 0 {
			if _sqlite3StrICmp(tls, zRight, __ccgo_ts+19016) == 0 {
				eMode2 = int32(SQLITE_CHECKPOINT_FULL)
			} else {
				if _sqlite3StrICmp(tls, zRight, __ccgo_ts+19875) == 0 {
					eMode2 = int32(SQLITE_CHECKPOINT_RESTART)
				} else {
					if _sqlite3StrICmp(tls, zRight, __ccgo_ts+19169) == 0 {
						eMode2 = int32(SQLITE_CHECKPOINT_TRUNCATE)
					} else {
						if _sqlite3StrICmp(tls, zRight, __ccgo_ts+19883) == 0 {
							eMode2 = -int32(1)
						}
					}
				}
			}
		}
		(*TParse)(unsafe.Pointer(pParse)).FnMem = int32(3)
		_sqlite3VdbeAddOp3(tls, v, int32(OP_Checkpoint), iBt, eMode2, int32(1))
		_sqlite3VdbeAddOp2(tls, v, int32(OP_ResultRow), int32(1), int32(3))
		break
		/*
		 **   PRAGMA wal_autocheckpoint
		 **   PRAGMA wal_autocheckpoint = N
		 **
		 ** Configure a database connection to automatically checkpoint a database
		 ** after accumulating N frames in the log. Or query for the current value
		 ** of N.
		 */
		fallthrough
	case int32(PragTyp_WAL_AUTOCHECKPOINT):
		if zRight != 0 {
			Xsqlite3_wal_autocheckpoint(tls, db, _sqlite3Atoi(tls, zRight))
		}
		if (*Tsqlite3)(unsafe.Pointer(db)).FxWalCallback == __ccgo_fp(_sqlite3WalDefaultHook) {
			v2 = int32(int64((*Tsqlite3)(unsafe.Pointer(db)).FpWalArg))
		} else {
			v2 = 0
		}
		_returnSingleInt(tls, v, int64(v2))
		break
		/*
		 **  PRAGMA shrink_memory
		 **
		 ** IMPLEMENTATION-OF: R-23445-46109 This pragma causes the database
		 ** connection on which it is invoked to free up as much memory as it
		 ** can, by calling sqlite3_db_release_memory().
		 */
		fallthrough
	case int32(PragTyp_SHRINK_MEMORY):
		Xsqlite3_db_release_memory(tls, db)
		break
		/*
		 **  PRAGMA optimize
		 **  PRAGMA optimize(MASK)
		 **  PRAGMA schema.optimize
		 **  PRAGMA schema.optimize(MASK)
		 **
		 ** Attempt to optimize the database.  All schemas are optimized in the first
		 ** two forms, and only the specified schema is optimized in the latter two.
		 **
		 ** The details of optimizations performed by this pragma are expected
		 ** to change and improve over time.  Applications should anticipate that
		 ** this pragma will perform new optimizations in future releases.
		 **
		 ** The optional argument is a bitmask of optimizations to perform:
		 **
		 **    0x00001    Debugging mode.  Do not actually perform any optimizations
		 **               but instead return one line of text for each optimization
		 **               that would have been done.  Off by default.
		 **
		 **    0x00002    Run ANALYZE on tables that might benefit.  On by default.
		 **               See below for additional information.
		 **
		 **    0x00010    Run all ANALYZE operations using an analysis_limit that
		 **               is the lessor of the current analysis_limit and the
		 **               SQLITE_DEFAULT_OPTIMIZE_LIMIT compile-time option.
		 **               The default value of SQLITE_DEFAULT_OPTIMIZE_LIMIT is
		 **               currently (2024-02-19) set to 2000, which is such that
		 **               the worst case run-time for PRAGMA optimize on a 100MB
		 **               database will usually be less than 100 milliseconds on
		 **               a RaspberryPI-4 class machine.  On by default.
		 **
		 **    0x10000    Look at tables to see if they need to be reanalyzed
		 **               due to growth or shrinkage even if they have not been
		 **               queried during the current connection.  Off by default.
		 **
		 ** The default MASK is and always shall be 0x0fffe.  In the current
		 ** implementation, the default mask only covers the 0x00002 optimization,
		 ** though additional optimizations that are covered by 0x0fffe might be
		 ** added in the future.  Optimizations that are off by default and must
		 ** be explicitly requested have masks of 0x10000 or greater.
		 **
		 ** DETERMINATION OF WHEN TO RUN ANALYZE
		 **
		 ** In the current implementation, a table is analyzed if only if all of
		 ** the following are true:
		 **
		 ** (1) MASK bit 0x00002 is set.
		 **
		 ** (2) The table is an ordinary table, not a virtual table or view.
		 **
		 ** (3) The table name does not begin with "sqlite_".
		 **
		 ** (4) One or more of the following is true:
		 **      (4a) The 0x10000 MASK bit is set.
		 **      (4b) One or more indexes on the table lacks an entry
		 **           in the sqlite_stat1 table.
		 **      (4c) The query planner used sqlite_stat1-style statistics for one
		 **           or more indexes of the table at some point during the lifetime
		 **           of the current connection.
		 **
		 ** (5) One or more of the following is true:
		 **      (5a) One or more indexes on the table lacks an entry
		 **           in the sqlite_stat1 table.  (Same as 4a)
		 **      (5b) The number of rows in the table has increased or decreased by
		 **           10-fold.  In other words, the current size of the table is
		 **           10 times larger than the size in sqlite_stat1 or else the
		 **           current size is less than 1/10th the size in sqlite_stat1.
		 **
		 ** The rules for when tables are analyzed are likely to change in
		 ** future releases.  Future versions of SQLite might accept a string
		 ** literal argument to this pragma that contains a mnemonic description
		 ** of the options rather than a bitmap.
		 */
		fallthrough
	case int32(PragTyp_OPTIMIZE): /* Analysis limit to use */
		nCheck = 0 /* Number of tables to be optimized */
		nBtree = 0 /* Number of indexes on the current table */
		if zRight != 0 {
			opMask = libc.Uint32FromInt32(_sqlite3Atoi(tls, zRight))
			if opMask&uint32(0x02) == uint32(0) {
				break
			}
		} else {
			opMask = uint32(0xfffe)
		}
		if opMask&uint32(0x10) == uint32(0) {
			nLimit = 0
		} else {
			if (*Tsqlite3)(unsafe.Pointer(db)).FnAnalysisLimit > 0 && (*Tsqlite3)(unsafe.Pointer(db)).FnAnalysisLimit < int32(SQLITE_DEFAULT_OPTIMIZE_LIMIT) {
				nLimit = 0
			} else {
				nLimit = int32(SQLITE_DEFAULT_OPTIMIZE_LIMIT)
			}
		}
		v1 = pParse + 56
		v2 = *(*int32)(unsafe.Pointer(v1))
		*(*int32)(unsafe.Pointer(v1)) = *(*int32)(unsafe.Pointer(v1)) + 1
		iTabCur = v2
		if zDb != 0 {
			v2 = iDb
		} else {
			v2 = (*Tsqlite3)(unsafe.Pointer(db)).FnDb - int32(1)
		}
		iDbLast = v2
		for {
			if !(iDb <= iDbLast) {
				break
			}
			if iDb == int32(1) {
				goto _71
			}
			_sqlite3CodeVerifySchema(tls, pParse, iDb)
			pSchema = (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(iDb)*32))).FpSchema
			k4 = (*THash)(unsafe.Pointer(pSchema + 8)).Ffirst
			for {
				if !(k4 != 0) {
					break
				}
				pTab12 = (*THashElem)(unsafe.Pointer(k4)).Fdata
				/* This only works for ordinary tables */
				if !(libc.Int32FromUint8((*TTable)(unsafe.Pointer(pTab12)).FeTabType) == libc.Int32FromInt32(TABTYP_NORM)) {
					goto _73
				}
				/* Do not scan system tables */
				if 0 == Xsqlite3_strnicmp(tls, (*TTable)(unsafe.Pointer(pTab12)).FzName, __ccgo_ts+6756, int32(7)) {
					goto _73
				}
				/* Find the size of the table as last recorded in sqlite_stat1.
				 ** If any index is unanalyzed, then the threshold is -1 to
				 ** indicate a new, unanalyzed index
				 */
				szThreshold = (*TTable)(unsafe.Pointer(pTab12)).FnRowLogEst
				nIndex = 0
				pIdx7 = (*TTable)(unsafe.Pointer(pTab12)).FpIndex
				for {
					if !(pIdx7 != 0) {
						break
					}
					nIndex = nIndex + 1
					if !(int32(uint32(*(*uint16)(unsafe.Pointer(pIdx7 + 100))&0x80>>7)) != 0) {
						szThreshold = int16(-int32(1)) /* Always analyze if any index lacks statistics */
					}
					goto _74
				_74:
					;
					pIdx7 = (*TIndex)(unsafe.Pointer(pIdx7)).FpNext
				}
				/* If table pTab has not been used in a way that would benefit from
				 ** having analysis statistics during the current session, then skip it,
				 ** unless the 0x10000 MASK bit is set. */
				if (*TTable)(unsafe.Pointer(pTab12)).FtabFlags&uint32(TF_MaybeReanalyze) != uint32(0) {
					/* Check for size change if stat1 has been used for a query */
				} else {
					if opMask&uint32(0x10000) != 0 {
						/* Check for size change if 0x10000 is set */
					} else {
						if (*TTable)(unsafe.Pointer(pTab12)).FpIndex != uintptr(0) && int32(szThreshold) < 0 {
							/* Do analysis if unanalyzed indexes exists */
						} else {
							/* Otherwise, we can skip this table */
							goto _73
						}
					}
				}
				nCheck = nCheck + 1
				if nCheck == int32(2) {
					/* If ANALYZE might be invoked two or more times, hold a write
					 ** transaction for efficiency */
					_sqlite3BeginWriteOperation(tls, pParse, 0, iDb)
				}
				nBtree = nBtree + (nIndex + int32(1))
				/* Reanalyze if the table is 10 times larger or smaller than
				 ** the last analysis.  Unconditional reanalysis if there are
				 ** unanalyzed indexes. */
				_sqlite3OpenTable(tls, pParse, iTabCur, iDb, pTab12, int32(OP_OpenRead))
				if int32(szThreshold) >= 0 {
					iRange = int16(33) /* 10x size change */
					if int32(szThreshold) >= int32(iRange) {
						v2 = int32(szThreshold) - int32(iRange)
					} else {
						v2 = -int32(1)
					}
					_sqlite3VdbeAddOp4Int(tls, v, int32(OP_IfSizeBetween), iTabCur, libc.Int32FromUint32(libc.Uint32FromInt32(_sqlite3VdbeCurrentAddr(tls, v)+int32(2))+opMask&uint32(1)), v2, int32(szThreshold)+int32(iRange))
				} else {
					_sqlite3VdbeAddOp2(tls, v, int32(OP_Rewind), iTabCur, libc.Int32FromUint32(libc.Uint32FromInt32(_sqlite3VdbeCurrentAddr(tls, v)+int32(2))+opMask&uint32(1)))
				}
				zSubSql = _sqlite3MPrintf(tls, db, __ccgo_ts+19888, libc.VaList(bp+176, (**(**TDb)(__ccgo_up((*Tsqlite3)(unsafe.Pointer(db)).FaDb + uintptr(iDb)*32))).FzDbSName, (*TTable)(unsafe.Pointer(pTab12)).FzName))
				if opMask&uint32(0x01) != 0 {
					r11 = _sqlite3GetTempReg(tls, pParse)
					_sqlite3VdbeAddOp4(tls, v, int32(OP_String8), 0, r11, 0, zSubSql, -int32(7))
					_sqlite3VdbeAddOp2(tls, v, int32(OP_ResultRow), r11, int32(1))
				} else {
					if nLimit != 0 {
						v2 = int32(0x02)
					} else {
						v2 = 00
					}
					_sqlite3VdbeAddOp4(tls, v, int32(OP_SqlExec), v2, nLimit, 0, zSubSql, -int32(7))
				}
				goto _73
			_73:
				;
				k4 = (*THashElem)(unsafe.Pointer(k4)).Fnext
			}
			goto _71
		_71:
			;
			iDb = iDb + 1
		}
		_sqlite3VdbeAddOp0(tls, v, int32(OP_Expire))
		/* In a schema with a large number of tables and indexes, scale back
		 ** the analysis_limit to avoid excess run-time in the worst case.
		 */
		if !((*Tsqlite3)(unsafe.Pointer(db)).FmallocFailed != 0) && nLimit > 0 && nBtree > int32(100) {
			nLimit = int32(100) * nLimit / nBtree
			if nLimit < int32(100) {
				nLimit = int32(100)
			}
			aOp5 = _sqlite3VdbeGetOp(tls, v, 0)
			iEnd = _sqlite3VdbeCurrentAddr(tls, v)
			iAddr1 = 0
			for {
				if !(iAddr1 < iEnd) {
					break
				}
				if libc.Int32FromUint8((**(**TVdbeOp)(__ccgo_up(aOp5 + uintptr(iAddr1)*24))).Fopcode) == int32(OP_SqlExec) {
					(**(**TVdbeOp)(__ccgo_up(aOp5 + uintptr(iAddr1)*24))).Fp2 = nLimit
				}
				goto _77
			_77:
				;
				iAddr1 = iAddr1 + 1
			}
		}
		break
		/*
		 **   PRAGMA busy_timeout
		 **   PRAGMA busy_timeout = N
		 **
		 ** Call sqlite3_busy_timeout(db, N).  Return the current timeout value
		 ** if one is set.  If no busy handler or a different busy handler is set
		 ** then 0 is returned.  Setting the busy_timeout to 0 or negative
		 ** disables the timeout.
		 */
		/*case PragTyp_BUSY_TIMEOUT*/
		fallthrough
	default:
		if zRight != 0 {
			Xsqlite3_busy_timeout(tls, db, _sqlite3Atoi(tls, zRight))
		}
		_returnSingleInt(tls, v, int64((*Tsqlite3)(unsafe.Pointer(db)).FbusyTimeout))
		break
		/*
		 **   PRAGMA soft_heap_limit
		 **   PRAGMA soft_heap_limit = N
		 **
		 ** IMPLEMENTATION-OF: R-26343-45930 This pragma invokes the
		 ** sqlite3_soft_heap_limit64() interface with the argument N, if N is
		 ** specified and is a non-negative integer.
		 ** IMPLEMENTATION-OF: R-64451-07163 The soft_heap_limit pragma always
		 ** returns the same integer that would be returned by the
		 ** sqlite3_soft_heap_limit64(-1) C-language function.
		 */
		fallthrough
	case int32(PragTyp_SOFT_HEAP_LIMIT):
		if zRight != 0 && _sqlite3DecOrHexToI64(tls, zRight, bp+136) == SQLITE_OK {
			Xsqlite3_soft_heap_limit64(tls, **(**Tsqlite3_int64)(__ccgo_up(bp + 136)))
		}
		_returnSingleInt(tls, v, Xsqlite3_soft_heap_limit64(tls, int64(-int32(1))))
		break
		/*
		 **   PRAGMA hard_heap_limit
		 **   PRAGMA hard_heap_limit = N
		 **
		 ** Invoke sqlite3_hard_heap_limit64() to query or set the hard heap
		 ** limit.  The hard heap limit can be activated or lowered by this
		 ** pragma, but not raised or deactivated.  Only the
		 ** sqlite3_hard_heap_limit64() C-language API can raise or deactivate
		 ** the hard heap limit.  This allows an application to set a heap limit
		 ** constraint that cannot be relaxed by an untrusted SQL script.
		 */
		fallthrough
	case int32(PragTyp_HARD_HEAP_LIMIT):
		if zRight != 0 && _sqlite3DecOrHexToI64(tls, zRight, bp+144) == SQLITE_OK {
			iPrior = Xsqlite3_hard_heap_limit64(tls, int64(-int32(1)))
			if **(**Tsqlite3_int64)(__ccgo_up(bp + 144)) > 0 && (iPrior == 0 || iPrior > **(**Tsqlite3_int64)(__ccgo_up(bp + 144))) {
				Xsqlite3_hard_heap_limit64(tls, **(**Tsqlite3_int64)(__ccgo_up(bp + 144)))
			}
		}
		_returnSingleInt(tls, v, Xsqlite3_hard_heap_limit64(tls, int64(-int32(1))))
		break
		/*
		 **   PRAGMA threads
		 **   PRAGMA threads = N
		 **
		 ** Configure the maximum number of worker threads.  Return the new
		 ** maximum, which might be less than requested.
		 */
		fallthrough
	case int32(PragTyp_THREADS):
		if zRight != 0 && _sqlite3DecOrHexToI64(tls, zRight, bp+152) == SQLITE_OK && **(**Tsqlite3_int64)(__ccgo_up(bp + 152)) >= 0 {
			Xsqlite3_limit(tls, db, int32(SQLITE_LIMIT_WORKER_THREADS), int32(**(**Tsqlite3_int64)(__ccgo_up(bp + 152))&libc.Int64FromInt32(0x7fffffff)))
		}
		_returnSingleInt(tls, v, int64(Xsqlite3_limit(tls, db, int32(SQLITE_LIMIT_WORKER_THREADS), -int32(1))))
		break
		/*
		 **   PRAGMA analysis_limit
		 **   PRAGMA analysis_limit = N
		 **
		 ** Configure the maximum number of rows that ANALYZE will examine
		 ** in each index that it looks at.  Return the new limit.
		 */
		fallthrough
	case int32(PragTyp_ANALYSIS_LIMIT):
		if zRight != 0 && _sqlite3DecOrHexToI64(tls, zRight, bp+160) == SQLITE_OK && **(**Tsqlite3_int64)(__ccgo_up(bp + 160)) >= 0 {
			(*Tsqlite3)(unsafe.Pointer(db)).FnAnalysisLimit = int32(**(**Tsqlite3_int64)(__ccgo_up(bp + 160)) & libc.Int64FromInt32(0x7fffffff))
		}
		_returnSingleInt(tls, v, int64((*Tsqlite3)(unsafe.Pointer(db)).FnAnalysisLimit)) /* IMP: R-57594-65522 */
		break
	} /* End of the PRAGMA switch */
	/* The following block is a no-op unless SQLITE_DEBUG is defined. Its only
	 ** purpose is to execute assert() statements to verify that if the
	 ** PragFlg_NoColumns1 flag is set and the caller specified an argument
	 ** to the PRAGMA, the implementation has not added any OP_ResultRow
	 ** instructions to the VM.  */
	if libc.Int32FromUint8((*TPragmaName)(unsafe.Pointer(pPragma)).FmPragFlg)&int32(PragFlg_NoColumns1) != 0 && zRight != 0 {
	}
	goto pragma_out
pragma_out:
	;
	_sqlite3DbFree(tls, db, zLeft)
	_sqlite3DbFree(tls, db, zRight)
}

// C documentation
//
//	/*
//	** Compile the UTF-16 encoded SQL statement zSql into a statement handle.
//	*/
func _sqlite3Prepare16(tls *libc.TLS, db uintptr, zSql uintptr, nBytes int32, prepFlags Tu32, ppStmt uintptr, pzTail uintptr) (r int32) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var chars_parsed, rc, sz, sz1 int32
	var z, z1, zSql8 uintptr
	var _ /* zTail8 at bp+0 */ uintptr
	_, _, _, _, _, _, _ = chars_parsed, rc, sz, sz1, z, z1, zSql8
	**(**uintptr)(__ccgo_up(bp)) = uintptr(0)
	rc = SQLITE_OK
	**(**uintptr)(__ccgo_up(ppStmt)) = uintptr(0)
	if !(_sqlite3SafetyCheckOk(tls, db) != 0) || zSql == uintptr(0) {
		return _sqlite3MisuseError(tls, int32(148771))
	}
	/* Make sure nBytes is non-negative and correct.  It should be the
	 ** number of bytes until the end of the input buffer or until the first
	 ** U+0000 character.  If the input nBytes is odd, convert it into
	 ** an even number.  If the input nBytes is negative, then the input
	 ** must be terminated by at least one U+0000 character */
	if nBytes >= 0 {
		z = zSql
		sz = 0
		for {
			if !(sz < nBytes && (libc.Int32FromUint8(**(**uint8)(__ccgo_up(z + uintptr(sz)))) != 0 || libc.Int32FromUint8(**(**uint8)(__ccgo_up(z + uintptr(sz+int32(1))))) != 0)) {
				break
			}
			goto _1
		_1:
			;
			sz = sz + int32(2)
		}
		nBytes = sz
	} else {
		z1 = zSql
		sz1 = 0
		for {
			if !(libc.Int32FromUint8(**(**uint8)(__ccgo_up(z1 + uintptr(sz1)))) != 0 || libc.Int32FromUint8(**(**uint8)(__ccgo_up(z1 + uintptr(sz1+int32(1))))) != 0) {
				break
			}
			goto _2
		_2:
			;
			sz1 = sz1 + int32(2)
		}
		nBytes = sz1
	}
	Xsqlite3_mutex_enter(tls, (*Tsqlite3)(unsafe.Pointer(db)).Fmutex)
	zSql8 = _sqlite3Utf16to8(tls, db, zSql, nBytes, uint8(SQLITE_UTF16BE))
	if zSql8 != 0 {
		rc = _sqlite3LockAndPrepare(tls, db, zSql8, -int32(1), prepFlags, uintptr(0), ppStmt, bp)
	}
	if **(**uintptr)(__ccgo_up(bp)) != 0 && pzTail != 0 {
		/* If sqlite3_prepare returns a tail pointer, we calculate the
		 ** equivalent pointer into the UTF-16 string by counting the unicode
		 ** characters between zSql8 and zTail8, and then returning a pointer
		 ** the same number of characters into the UTF-16 string.
		 */
		chars_parsed = _sqlite3Utf8CharLen(tls, zSql8, int32(int64(**(**uintptr)(__ccgo_up(bp)))-int64(zSql8)))
		**(**uintptr)(__ccgo_up(pzTail)) = zSql + uintptr(_sqlite3Utf16ByteLen(tls, zSql, nBytes, chars_parsed))
	}
	_sqlite3DbFree(tls, db, zSql8)
	rc = _sqlite3ApiExit(tls, db, rc)
	Xsqlite3_mutex_leave(tls, (*Tsqlite3)(unsafe.Pointer(db)).Fmutex)
	return rc
}

func _sqlite3Put4byte(tls *libc.TLS, p uintptr, _v Tu32) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	*(*Tu32)(unsafe.Pointer(bp)) = _v
	libc.Xmemcpy(tls, p, bp, uint64(4))
}

// C documentation
//
//	/*
//	** zIn is a UTF-16 encoded unicode string at least nByte bytes long.
//	** Return the number of bytes in the first nChar unicode characters
//	** in pZ.  nChar must be non-negative.  Surrogate pairs count as a single
//	** character.
//	*/
func _sqlite3Utf16ByteLen(tls *libc.TLS, zIn uintptr, nByte int32, nChar int32) (r int32) {
	var c, n int32
	var z, zEnd uintptr
	_, _, _, _ = c, n, z, zEnd
	z = zIn
	zEnd = z + uintptr(nByte-int32(1))
	n = 0
	if false {
		z = z + 1
	}
	for n < nChar && z <= zEnd {
		c = libc.Int32FromUint8(**(**uint8)(__ccgo_up(z)))
		z = z + uintptr(2)
		if c >= int32(0xd8) && c < int32(0xdc) && z <= zEnd && libc.Int32FromUint8(**(**uint8)(__ccgo_up(z))) >= int32(0xdc) && libc.Int32FromUint8(**(**uint8)(__ccgo_up(z))) < int32(0xe0) {
			z = z + uintptr(2)
		}
		n = n + 1
	}
	return int32(int64(z)-int64(zIn)) - libc.BoolInt32(false)
}

// C documentation
//
//	/*
//	** Check to see if the frame with header in aFrame[] and content
//	** in aData[] is valid.  If it is a valid frame, fill *piPage and
//	** *pnTruncate and return true.  Return if the frame is not valid.
//	*/
func _walDecodeFrame(tls *libc.TLS, pWal uintptr, piPage uintptr, pnTruncate uintptr, aData uintptr, aFrame uintptr) (r int32) {
	var aCksum uintptr
	var nativeCksum int32
	var pgno Tu32
	_, _, _ = aCksum, nativeCksum, pgno /* True for native byte-order checksums */
	aCksum = pWal + 72 + 24             /* Page number of the frame */
	/* A frame is only valid if the salt values in the frame-header
	 ** match the salt values in the wal-header.
	 */
	if libc.Xmemcmp(tls, pWal+72+32, aFrame+8, uint64(8)) != 0 {
		return 0
	}
	/* A frame is only valid if the page number is greater than zero.
	 */
	pgno = _sqlite3Get4byte(tls, aFrame)
	if pgno == uint32(0) {
		return 0
	}
	/* A frame is only valid if a checksum of the WAL header,
	 ** all prior frames, the first 16 bytes of this frame-header,
	 ** and the frame-data matches the checksum in the last 8
	 ** bytes of this frame-header.
	 */
	nativeCksum = libc.BoolInt32(libc.Int32FromUint8((*TWal)(unsafe.Pointer(pWal)).Fhdr.FbigEndCksum) == int32(SQLITE_BIGENDIAN))
	_walChecksumBytes(tls, nativeCksum, aFrame, int32(8), aCksum, aCksum)
	_walChecksumBytes(tls, nativeCksum, aData, libc.Int32FromUint32((*TWal)(unsafe.Pointer(pWal)).FszPage), aCksum, aCksum)
	if **(**Tu32)(__ccgo_up(aCksum)) != _sqlite3Get4byte(tls, aFrame+16) || **(**Tu32)(__ccgo_up(aCksum + 1*4)) != _sqlite3Get4byte(tls, aFrame+20) {
		/* Checksum failed. */
		return 0
	}
	/* If we reach this point, the frame is valid.  Return the page number
	 ** and the new database size.
	 */
	**(**Tu32)(__ccgo_up(piPage)) = pgno
	**(**Tu32)(__ccgo_up(pnTruncate)) = _sqlite3Get4byte(tls, aFrame+4)
	return int32(1)
}

// C documentation
//
//	/*
//	** This function encodes a single frame header and writes it to a buffer
//	** supplied by the caller. A frame-header is made up of a series of
//	** 4-byte big-endian integers, as follows:
//	**
//	**     0: Page number.
//	**     4: For commit records, the size of the database image in pages
//	**        after the commit. For all other records, zero.
//	**     8: Salt-1 (copied from the wal-header)
//	**    12: Salt-2 (copied from the wal-header)
//	**    16: Checksum-1.
//	**    20: Checksum-2.
//	*/
func _walEncodeFrame(tls *libc.TLS, pWal uintptr, iPage Tu32, nTruncate Tu32, aData uintptr, aFrame uintptr) {
	var aCksum uintptr
	var nativeCksum int32
	_, _ = aCksum, nativeCksum /* True for native byte-order checksums */
	aCksum = pWal + 72 + 24
	_sqlite3Put4byte(tls, aFrame, iPage)
	_sqlite3Put4byte(tls, aFrame+4, nTruncate)
	if (*TWal)(unsafe.Pointer(pWal)).FiReCksum == uint32(0) {
		libc.Xmemcpy(tls, aFrame+8, pWal+72+32, uint64(8))
		nativeCksum = libc.BoolInt32(libc.Int32FromUint8((*TWal)(unsafe.Pointer(pWal)).Fhdr.FbigEndCksum) == int32(SQLITE_BIGENDIAN))
		_walChecksumBytes(tls, nativeCksum, aFrame, int32(8), aCksum, aCksum)
		_walChecksumBytes(tls, nativeCksum, aData, libc.Int32FromUint32((*TWal)(unsafe.Pointer(pWal)).FszPage), aCksum, aCksum)
		_sqlite3Put4byte(tls, aFrame+16, **(**Tu32)(__ccgo_up(aCksum)))
		_sqlite3Put4byte(tls, aFrame+20, **(**Tu32)(__ccgo_up(aCksum + 1*4)))
	} else {
		libc.Xmemset(tls, aFrame+8, 0, uint64(16))
	}
}

// C documentation
//
//	/*
//	** Recover the wal-index by reading the write-ahead log file.
//	**
//	** This routine first tries to establish an exclusive lock on the
//	** wal-index to prevent other threads/processes from doing anything
//	** with the WAL or wal-index while recovery is running.  The
//	** WAL_RECOVER_LOCK is also held so that other threads will know
//	** that this thread is running recovery.  If unable to establish
//	** the necessary locks, this routine returns SQLITE_BUSY.
//	*/
func _walIndexRecover(tls *libc.TLS, pWal uintptr) (r int32) {
	bp := tls.Alloc(80)
	defer tls.Free(80)
	var aData, aFrame, aPrivate, pInfo uintptr
	var aFrameCksum [2]Tu32
	var i, iLock, isValid, rc, szFrame, szPage int32
	var iFirst, iFrame, iLast, iLastFrame, iPg, magic, nHdr, nHdr32, version Tu32
	var iOffset Ti64
	var v2, v3 uint64
	var _ /* aBuf at bp+8 */ [32]Tu8
	var _ /* aShare at bp+40 */ uintptr
	var _ /* nSize at bp+0 */ Ti64
	var _ /* nTruncate at bp+52 */ Tu32
	var _ /* pgno at bp+48 */ Tu32
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _, _ = aData, aFrame, aFrameCksum, aPrivate, i, iFirst, iFrame, iLast, iLastFrame, iLock, iOffset, iPg, isValid, magic, nHdr, nHdr32, pInfo, rc, szFrame, szPage, version, v2, v3 /* Size of log file */
	aFrameCksum = [2]Tu32{}                                                                                                                                                                                                                          /* Lock offset to lock for checkpoint */
	/* Obtain an exclusive lock on all byte in the locking range not already
	 ** locked by the caller. The caller is guaranteed to have locked the
	 ** WAL_WRITE_LOCK byte, and may have also locked the WAL_CKPT_LOCK byte.
	 ** If successful, the same bytes that are locked here are unlocked before
	 ** this function returns.
	 */
	iLock = int32(WAL_ALL_BUT_WRITE) + libc.Int32FromUint8((*TWal)(unsafe.Pointer(pWal)).FckptLock)
	rc = _walLockExclusive(tls, pWal, iLock, libc.Int32FromInt32(3)+libc.Int32FromInt32(0)-iLock)
	if rc != 0 {
		return rc
	}
	libc.Xmemset(tls, pWal+72, 0, uint64(48))
	rc = _sqlite3OsFileSize(tls, (*TWal)(unsafe.Pointer(pWal)).FpWalFd, bp)
	if rc != SQLITE_OK {
		goto recovery_error
	}
	if **(**Ti64)(__ccgo_up(bp)) > int64(WAL_HDRSIZE) { /* Buffer to load WAL header into */
		aPrivate = uintptr(0) /* Heap copy of *-shm hash being populated */
		aFrame = uintptr(0)   /* Last frame in wal, based on nSize alone */
		/* Read in the WAL header. */
		rc = _sqlite3OsRead(tls, (*TWal)(unsafe.Pointer(pWal)).FpWalFd, bp+8, int32(WAL_HDRSIZE), 0)
		if rc != SQLITE_OK {
			goto recovery_error
		}
		/* If the database page size is not a power of two, or is greater than
		 ** SQLITE_MAX_PAGE_SIZE, conclude that the WAL file contains no valid
		 ** data. Similarly, if the 'magic' value is invalid, ignore the whole
		 ** WAL file.
		 */
		magic = _sqlite3Get4byte(tls, bp+8)
		szPage = libc.Int32FromUint32(_sqlite3Get4byte(tls, bp+8+8))
		if magic&uint32(0xFFFFFFFE) != uint32(WAL_MAGIC) || szPage&(szPage-int32(1)) != 0 || szPage > int32(SQLITE_MAX_PAGE_SIZE) || szPage < int32(512) {
			goto finished
		}
		(*TWal)(unsafe.Pointer(pWal)).Fhdr.FbigEndCksum = uint8(magic & libc.Uint32FromInt32(0x00000001))
		(*TWal)(unsafe.Pointer(pWal)).FszPage = libc.Uint32FromInt32(szPage)
		(*TWal)(unsafe.Pointer(pWal)).FnCkpt = _sqlite3Get4byte(tls, bp+8+12)
		libc.Xmemcpy(tls, pWal+72+32, bp+8+16, uint64(8))
		/* Verify that the WAL header checksum is correct */
		_walChecksumBytes(tls, libc.BoolInt32(libc.Int32FromUint8((*TWal)(unsafe.Pointer(pWal)).Fhdr.FbigEndCksum) == int32(SQLITE_BIGENDIAN)), bp+8, libc.Int32FromInt32(WAL_HDRSIZE)-libc.Int32FromInt32(2)*libc.Int32FromInt32(4), uintptr(0), pWal+72+24)
		if **(**Tu32)(__ccgo_up(pWal + 72 + 24)) != _sqlite3Get4byte(tls, bp+8+24) || **(**Tu32)(__ccgo_up(pWal + 72 + 24 + 1*4)) != _sqlite3Get4byte(tls, bp+8+28) {
			goto finished
		}
		/* Verify that the version number on the WAL format is one that
		 ** are able to understand */
		version = _sqlite3Get4byte(tls, bp+8+4)
		if version != uint32(WAL_MAX_VERSION) {
			rc = _sqlite3CantopenError(tls, int32(68910))
			goto finished
		}
		/* Malloc a buffer to read frames into. */
		szFrame = szPage + int32(WAL_FRAME_HDRSIZE)
		aFrame = Xsqlite3_malloc64(tls, uint64(libc.Uint64FromInt32(szFrame)+(libc.Uint64FromInt64(2)*libc.Uint64FromInt32(libc.Int32FromInt32(HASHTABLE_NPAGE)*libc.Int32FromInt32(2))+libc.Uint64FromInt32(HASHTABLE_NPAGE)*libc.Uint64FromInt64(4))))
		if !(aFrame != 0) {
			rc = int32(SQLITE_NOMEM)
			goto recovery_error
		}
		aData = aFrame + 24
		aPrivate = aData + uintptr(szPage)
		/* Read all frames from the log file. */
		iLastFrame = libc.Uint32FromInt64((**(**Ti64)(__ccgo_up(bp)) - int64(WAL_HDRSIZE)) / int64(szFrame))
		iPg = uint32(0)
		for {
			if !(iPg <= libc.Uint32FromInt32(_walFramePage(tls, iLastFrame))) {
				break
			}
			if uint64(iLastFrame) < libc.Uint64FromInt32(HASHTABLE_NPAGE)-(libc.Uint64FromInt64(48)*libc.Uint64FromInt32(2)+libc.Uint64FromInt64(40))/libc.Uint64FromInt64(4)+uint64(iPg*uint32(HASHTABLE_NPAGE)) {
				v2 = uint64(iLastFrame)
			} else {
				v2 = libc.Uint64FromInt32(HASHTABLE_NPAGE) - (libc.Uint64FromInt64(48)*libc.Uint64FromInt32(2)+libc.Uint64FromInt64(40))/libc.Uint64FromInt64(4) + uint64(iPg*uint32(HASHTABLE_NPAGE))
			} /* Index of last frame read */
			iLast = uint32(v2)
			if iPg == uint32(0) {
				v3 = uint64(0)
			} else {
				v3 = libc.Uint64FromInt32(HASHTABLE_NPAGE) - (libc.Uint64FromInt64(48)*libc.Uint64FromInt32(2)+libc.Uint64FromInt64(40))/libc.Uint64FromInt64(4) + uint64((iPg-uint32(1))*uint32(HASHTABLE_NPAGE))
			}
			iFirst = uint32(uint64(1) + v3)
			rc = _walIndexPage(tls, pWal, libc.Int32FromUint32(iPg), bp+40)
			if **(**uintptr)(__ccgo_up(bp + 40)) == uintptr(0) {
				break
			}
			**(**uintptr)(__ccgo_up((*TWal)(unsafe.Pointer(pWal)).FapWiData + uintptr(iPg)*8)) = aPrivate
			iFrame = iFirst
			for {
				if !(iFrame <= iLast) {
					break
				}
				iOffset = libc.Int64FromInt32(WAL_HDRSIZE) + libc.Int64FromUint32(iFrame-libc.Uint32FromInt32(1))*int64(szPage+libc.Int32FromInt32(WAL_FRAME_HDRSIZE)) /* dbsize field from frame header */
				/* Read and decode the next log frame. */
				rc = _sqlite3OsRead(tls, (*TWal)(unsafe.Pointer(pWal)).FpWalFd, aFrame, szFrame, iOffset)
				if rc != SQLITE_OK {
					break
				}
				isValid = _walDecodeFrame(tls, pWal, bp+48, bp+52, aData, aFrame)
				if !(isValid != 0) {
					break
				}
				rc = _walIndexAppend(tls, pWal, iFrame, **(**Tu32)(__ccgo_up(bp + 48)))
				if rc != SQLITE_OK {
					break
				}
				/* If nTruncate is non-zero, this is a commit record. */
				if **(**Tu32)(__ccgo_up(bp + 52)) != 0 {
					(*TWal)(unsafe.Pointer(pWal)).Fhdr.FmxFrame = iFrame
					(*TWal)(unsafe.Pointer(pWal)).Fhdr.FnPage = **(**Tu32)(__ccgo_up(bp + 52))
					(*TWal)(unsafe.Pointer(pWal)).Fhdr.FszPage = libc.Uint16FromInt32(szPage&libc.Int32FromInt32(0xff00) | szPage>>libc.Int32FromInt32(16))
					aFrameCksum[0] = **(**Tu32)(__ccgo_up(pWal + 72 + 24))
					aFrameCksum[int32(1)] = **(**Tu32)(__ccgo_up(pWal + 72 + 24 + 1*4))
				}
				goto _4
			_4:
				;
				iFrame = iFrame + 1
			}
			**(**uintptr)(__ccgo_up((*TWal)(unsafe.Pointer(pWal)).FapWiData + uintptr(iPg)*8)) = **(**uintptr)(__ccgo_up(bp + 40))
			if iPg == uint32(0) {
				v2 = libc.Uint64FromInt64(48)*libc.Uint64FromInt32(2) + libc.Uint64FromInt64(40)
			} else {
				v2 = uint64(0)
			}
			nHdr = uint32(v2)
			nHdr32 = uint32(uint64(nHdr) / uint64(4))
			/* Memcpy() should work fine here, on all reasonable implementations.
			 ** Technically, memcpy() might change the destination to some
			 ** intermediate value before setting to the final value, and that might
			 ** cause a concurrent reader to malfunction.  Memcpy() is allowed to
			 ** do that, according to the spec, but no memcpy() implementation that
			 ** we know of actually does that, which is why we say that memcpy()
			 ** is safe for this.  Memcpy() is certainly a lot faster.
			 */
			libc.Xmemcpy(tls, **(**uintptr)(__ccgo_up(bp + 40))+uintptr(nHdr32)*4, aPrivate+uintptr(nHdr32)*4, libc.Uint64FromInt64(2)*libc.Uint64FromInt32(libc.Int32FromInt32(HASHTABLE_NPAGE)*libc.Int32FromInt32(2))+libc.Uint64FromInt32(HASHTABLE_NPAGE)*libc.Uint64FromInt64(4)-uint64(nHdr))
			if iFrame <= iLast {
				break
			}
			goto _1
		_1:
			;
			iPg = iPg + 1
		}
		Xsqlite3_free(tls, aFrame)
	}
	goto finished
finished:
	;
	if rc == SQLITE_OK {
		**(**Tu32)(__ccgo_up(pWal + 72 + 24)) = aFrameCksum[0]
		**(**Tu32)(__ccgo_up(pWal + 72 + 24 + 1*4)) = aFrameCksum[int32(1)]
		_walIndexWriteHdr(tls, pWal)
		/* Reset the checkpoint-header. This is safe because this thread is
		 ** currently holding locks that exclude all other writers and
		 ** checkpointers. Then set the values of read-mark slots 1 through N.
		 */
		pInfo = _walCkptInfo(tls, pWal)
		(*TWalCkptInfo)(unsafe.Pointer(pInfo)).FnBackfill = uint32(0)
		(*TWalCkptInfo)(unsafe.Pointer(pInfo)).FnBackfillAttempted = (*TWal)(unsafe.Pointer(pWal)).Fhdr.FmxFrame
		**(**Tu32)(__ccgo_up(pInfo + 4)) = uint32(0)
		i = int32(1)
		for {
			if !(i < libc.Int32FromInt32(SQLITE_SHM_NLOCK)-libc.Int32FromInt32(3)) {
				break
			}
			rc = _walLockExclusive(tls, pWal, int32(3)+i, int32(1))
			if rc == SQLITE_OK {
				if i == int32(1) && (*TWal)(unsafe.Pointer(pWal)).Fhdr.FmxFrame != 0 {
					**(**Tu32)(__ccgo_up(pInfo + 4 + uintptr(i)*4)) = (*TWal)(unsafe.Pointer(pWal)).Fhdr.FmxFrame
				} else {
					**(**Tu32)(__ccgo_up(pInfo + 4 + uintptr(i)*4)) = uint32(READMARK_NOT_USED)
				}
				_walUnlockExclusive(tls, pWal, int32(3)+i, int32(1))
			} else {
				if rc != int32(SQLITE_BUSY) {
					goto recovery_error
				}
			}
			goto _6
		_6:
			;
			i = i + 1
		}
		/* If more than one frame was recovered from the log file, report an
		 ** event via sqlite3_log(). This is to help with identifying performance
		 ** problems caused by applications routinely shutting down without
		 ** checkpointing the log file.
		 */
		if (*TWal)(unsafe.Pointer(pWal)).Fhdr.FnPage != 0 {
			Xsqlite3_log(tls, libc.Int32FromInt32(SQLITE_NOTICE)|libc.Int32FromInt32(1)<<libc.Int32FromInt32(8), __ccgo_ts+4276, libc.VaList(bp+64, (*TWal)(unsafe.Pointer(pWal)).Fhdr.FmxFrame, (*TWal)(unsafe.Pointer(pWal)).FzWalName))
		}
	}
	goto recovery_error
recovery_error:
	;
	_walUnlockExclusive(tls, pWal, iLock, libc.Int32FromInt32(3)+libc.Int32FromInt32(0)-iLock)
	return rc
}

func _writeCoord(tls *libc.TLS, p uintptr, pCoord uintptr) (r int32) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	var _ /* i at bp+0 */ Tu32
	**(**Tu32)(__ccgo_up(bp)) = *(*Tu32)(unsafe.Pointer(pCoord))
	libc.Xmemcpy(tls, p, bp, uint64(4))
	return int32(4)
}

func _writeInt64(tls *libc.TLS, p uintptr, _i Ti64) (r int32) {
	bp := tls.Alloc(16)
	defer tls.Free(16)
	*(*Ti64)(unsafe.Pointer(bp)) = _i
	libc.Xmemcpy(tls, p, bp, uint64(8))
	return int32(8)
}
