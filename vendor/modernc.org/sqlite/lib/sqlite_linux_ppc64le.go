// Code generated for linux/ppc64le by 'generator -D_GCC_NULLPTR_T -D_Float16=short -D__bf16=short -mlong-double-64 --package-name libsqlite3 --prefix-enumerator=_ --prefix-external=x_ --prefix-field=F --prefix-static-internal=_ --prefix-static-none=_ --prefix-tagged-enum=_ --prefix-tagged-struct=T --prefix-tagged-union=T --prefix-typename=T --prefix-undefined=_ -ignore-unsupported-alignment -ignore-link-errors -import=sync -DHAVE_USLEEP -DLONGDOUBLE_TYPE=double -DNDEBUG -DSQLITE_DEFAULT_MEMSTATUS=0 -DSQLITE_DISABLE_INTRINSIC -DSQLITE_ENABLE_COLUMN_METADATA -DSQLITE_ENABLE_DBPAGE_VTAB -DSQLITE_ENABLE_DBSTAT_VTAB -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_GEOPOLY -DSQLITE_ENABLE_JSON1 -DSQLITE_ENABLE_MATH_FUNCTIONS -DSQLITE_ENABLE_MEMORY_MANAGEMENT -DSQLITE_ENABLE_OFFSET_SQL_FUNC -DSQLITE_ENABLE_PREUPDATE_HOOK -DSQLITE_ENABLE_RBU -DSQLITE_ENABLE_RTREE -DSQLITE_ENABLE_SESSION -DSQLITE_ENABLE_SNAPSHOT -DSQLITE_ENABLE_STAT4 -DSQLITE_ENABLE_UNLOCK_NOTIFY -DSQLITE_HAVE_ZLIB=1 -DSQLITE_LIKE_DOESNT_MATCH_BLOBS -DSQLITE_SOUNDEX -DSQLITE_THREADSAFE=1 -DSQLITE_WITHOUT_ZONEMALLOC -D_LARGEFILE64_SOURCE -I /home/debian/src/modernc.org/builder/.exclude/modernc.org/libc/include/linux/ppc64le -I /home/debian/src/modernc.org/builder/.exclude/modernc.org/libz/include/linux/ppc64le -I /home/debian/src/modernc.org/builder/.exclude/modernc.org/libtcl8.6/include/linux/ppc64le -extended-errors -o sqlite3.go sqlite3.c -DSQLITE_OS_UNIX=1 -eval-all-macros', DO NOT EDIT.

//go:build linux && ppc64le

package sqlite3

const EDEADLOCK = 58

const F2FS_IOC_ABORT_VOLATILE_WRITE = 536933637

const F2FS_IOC_COMMIT_ATOMIC_WRITE = 536933634

const F2FS_IOC_GET_FEATURES = 1073804556

const F2FS_IOC_START_ATOMIC_WRITE = 536933633

const F2FS_IOC_START_VOLATILE_WRITE = 536933635

const FIOQSIZE = 1073768064

const O_DIRECT = 131072

const O_LARGEFILE = 65536

const PROT_SAO = 16

const TCFLSH = 536900639

const TCGETA = 1073771543

const TCGETS = 1073771539

const TCSBRK = 536900637

const TCSETA = 2147513368

const TCSETAF = 2147513372

const TCSETAW = 2147513369

const TCSETS = 2147513364

const TCSETSF = 2147513366

const TCSETSW = 2147513365

const TCXONC = 536900638

const TIOCGDEV = 1073763378

const TIOCGETC = 1073771538

const TIOCGETP = 1073771528

const TIOCGEXCL = 1073763392

const TIOCGLTC = 1073771636

const TIOCGPKT = 1073763384

const TIOCGPTLCK = 1073763385

const TIOCGPTN = 1073763376

const TIOCGPTPEER = 536892481

const TIOCINQ = 1073768063

const TIOCSETC = 2147513361

const TIOCSETN = 2147513354

const TIOCSETP = 2147513353

const TIOCSIG = 2147505206

const TIOCSLTC = 2147513461

const TIOCSPTLCK = 2147505201

type Tstat = struct {
	Fst_dev     Tdev_t
	Fst_ino     Tino_t
	Fst_nlink   Tnlink_t
	Fst_mode    Tmode_t
	Fst_uid     Tuid_t
	Fst_gid     Tgid_t
	Fst_rdev    Tdev_t
	Fst_size    Toff_t
	Fst_blksize Tblksize_t
	Fst_blocks  Tblkcnt_t
	Fst_atim    Ttimespec
	Fst_mtim    Ttimespec
	Fst_ctim    Ttimespec
	F__unused   [3]uint64
}

const _ARCH_PPC = 1

const _ARCH_PPC64 = 1

const _ARCH_PPCGR = 1

const _ARCH_PPCSQ = 1

const _ARCH_PWR4 = 1

const _ARCH_PWR5 = 1

const _ARCH_PWR5X = 1

const _ARCH_PWR6 = 1

const _ARCH_PWR7 = 1

const _ARCH_PWR8 = 1

const _CALL_ELF = 2

const _CALL_LINUX = 1

const _IOC_NONE = 1

const _IOC_WRITE = 4

const _LITTLE_ENDIAN = 1

const __ALTIVEC__ = 1

const __APPLE_ALTIVEC__ = 1

const __BUILTIN_CPU_SUPPORTS__ = 1

const __CMODEL_MEDIUM__ = 1

const __CRYPTO__ = 1

const __HAVE_BSWAP__ = 1

const __POWER8_VECTOR__ = 1

const __PPC64__ = 1

const __PPC__ = 1

const __QUAD_MEMORY_ATOMIC__ = 1

const __RECIPF__ = 1

const __RECIP_PRECISION__ = 1

const __RECIP__ = 1

const __RSQRTEF__ = 1

const __RSQRTE__ = 1

const __SET_FPSCR_RN_RETURNS_FPSCR__ = 1

const __SIZEOF_IEEE128__ = 16

const __STRUCT_PARM_ALIGN__ = 16

const __VEC_ELEMENT_REG_ORDER__ = 1234

const __VEC__ = 10206

const __VSX__ = 1

const __builtin_vsx_vperm = 0

const __builtin_vsx_xvmaddadp = 0

const __builtin_vsx_xvmaddasp = 0

const __builtin_vsx_xvmaddmdp = 0

const __builtin_vsx_xvmaddmsp = 0

const __builtin_vsx_xvmsubadp = 0

const __builtin_vsx_xvmsubasp = 0

const __builtin_vsx_xvmsubmdp = 0

const __builtin_vsx_xvmsubmsp = 0

const __builtin_vsx_xvnmaddadp = 0

const __builtin_vsx_xvnmaddasp = 0

const __builtin_vsx_xvnmaddmdp = 0

const __builtin_vsx_xvnmaddmsp = 0

const __builtin_vsx_xvnmsubadp = 0

const __builtin_vsx_xvnmsubasp = 0

const __builtin_vsx_xvnmsubmdp = 0

const __builtin_vsx_xvnmsubmsp = 0

const __builtin_vsx_xxland = 0

const __builtin_vsx_xxlandc = 0

const __builtin_vsx_xxlnor = 0

const __builtin_vsx_xxlor = 0

const __builtin_vsx_xxlxor = 0

const __builtin_vsx_xxsel = 0

const __float128 = 0

const __powerpc64__ = 1

const __powerpc__ = 1
