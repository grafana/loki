// Code generated for linux/ppc64le by 'generator -D_GCC_NULLPTR_T -D_Float16=short -D__bf16=short -mlong-double-64 --package-name libsqlite3 --prefix-enumerator=_ --prefix-external=x_ --prefix-field=F --prefix-static-internal=_ --prefix-static-none=_ --prefix-tagged-enum=_ --prefix-tagged-struct=T --prefix-tagged-union=T --prefix-typename=T --prefix-undefined=_ -ignore-unsupported-alignment -ignore-link-errors -import=sync -DHAVE_USLEEP -DLONGDOUBLE_TYPE=double -DNDEBUG -DSQLITE_DEFAULT_MEMSTATUS=0 -DSQLITE_DISABLE_INTRINSIC -DSQLITE_ENABLE_COLUMN_METADATA -DSQLITE_ENABLE_DBPAGE_VTAB -DSQLITE_ENABLE_DBSTAT_VTAB -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_GEOPOLY -DSQLITE_ENABLE_JSON1 -DSQLITE_ENABLE_MATH_FUNCTIONS -DSQLITE_ENABLE_MEMORY_MANAGEMENT -DSQLITE_ENABLE_OFFSET_SQL_FUNC -DSQLITE_ENABLE_PREUPDATE_HOOK -DSQLITE_ENABLE_RBU -DSQLITE_ENABLE_RTREE -DSQLITE_ENABLE_SESSION -DSQLITE_ENABLE_SNAPSHOT -DSQLITE_ENABLE_STAT4 -DSQLITE_ENABLE_UNLOCK_NOTIFY -DSQLITE_HAVE_ZLIB=1 -DSQLITE_LIKE_DOESNT_MATCH_BLOBS -DSQLITE_SOUNDEX -DSQLITE_THREADSAFE=1 -DSQLITE_WITHOUT_ZONEMALLOC -D_LARGEFILE64_SOURCE -I /home/debian/src/modernc.org/builder/.exclude/modernc.org/libc/include/linux/ppc64le -I /home/debian/src/modernc.org/builder/.exclude/modernc.org/libz/include/linux/ppc64le -I /home/debian/src/modernc.org/builder/.exclude/modernc.org/libtcl8.6/include/linux/ppc64le -extended-errors -o sqlite3.go sqlite3.c -DSQLITE_OS_UNIX=1 -eval-all-macros', DO NOT EDIT.

//go:build linux && ppc64le

package sqlite3

const EDEADLOCK = 58

const F2FS_IOC_ABORT_VOLATILE_WRITE = 536933637

const F2FS_IOC_COMMIT_ATOMIC_WRITE = 536933634

const F2FS_IOC_GET_FEATURES = 1074066700

const F2FS_IOC_START_ATOMIC_WRITE = 536933633

const F2FS_IOC_START_VOLATILE_WRITE = 536933635

const FIOQSIZE = 1074292352

const O_DIRECT = 131072

const O_LARGEFILE = 65536

const PROT_SAO = 16

const TCFLSH = 536900639

const TCGETA = 1075082263

const TCGETS = 1076655123

const TCSBRK = 536900637

const TCSETA = 2148824088

const TCSETAF = 2148824092

const TCSETAW = 2148824089

const TCSETS = 2150396948

const TCSETSF = 2150396950

const TCSETSW = 2150396949

const TCXONC = 536900638

const TIOCGDEV = 1074025522

const TIOCGETC = 1074164754

const TIOCGETP = 1074164744

const TIOCGEXCL = 1074025536

const TIOCGLTC = 1074164852

const TIOCGPKT = 1074025528

const TIOCGPTLCK = 1074025529

const TIOCGPTN = 1074025520

const TIOCGPTPEER = 536892481

const TIOCINQ = 1074030207

const TIOCSETC = 2147906577

const TIOCSETN = 2147906570

const TIOCSETP = 2147906569

const TIOCSIG = 2147767350

const TIOCSLTC = 2147906677

const TIOCSPTLCK = 2147767345

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

const __builtin_vsx_vperm = "__builtin_vec_perm"

const __builtin_vsx_xvmaddadp = "__builtin_vsx_xvmadddp"

const __builtin_vsx_xvmaddasp = "__builtin_vsx_xvmaddsp"

const __builtin_vsx_xvmaddmdp = "__builtin_vsx_xvmadddp"

const __builtin_vsx_xvmaddmsp = "__builtin_vsx_xvmaddsp"

const __builtin_vsx_xvmsubadp = "__builtin_vsx_xvmsubdp"

const __builtin_vsx_xvmsubasp = "__builtin_vsx_xvmsubsp"

const __builtin_vsx_xvmsubmdp = "__builtin_vsx_xvmsubdp"

const __builtin_vsx_xvmsubmsp = "__builtin_vsx_xvmsubsp"

const __builtin_vsx_xvnmaddadp = "__builtin_vsx_xvnmadddp"

const __builtin_vsx_xvnmaddasp = "__builtin_vsx_xvnmaddsp"

const __builtin_vsx_xvnmaddmdp = "__builtin_vsx_xvnmadddp"

const __builtin_vsx_xvnmaddmsp = "__builtin_vsx_xvnmaddsp"

const __builtin_vsx_xvnmsubadp = "__builtin_vsx_xvnmsubdp"

const __builtin_vsx_xvnmsubasp = "__builtin_vsx_xvnmsubsp"

const __builtin_vsx_xvnmsubmdp = "__builtin_vsx_xvnmsubdp"

const __builtin_vsx_xvnmsubmsp = "__builtin_vsx_xvnmsubsp"

const __builtin_vsx_xxland = "__builtin_vec_and"

const __builtin_vsx_xxlandc = "__builtin_vec_andc"

const __builtin_vsx_xxlnor = "__builtin_vec_nor"

const __builtin_vsx_xxlor = "__builtin_vec_or"

const __builtin_vsx_xxlxor = "__builtin_vec_xor"

const __builtin_vsx_xxsel = "__builtin_vec_sel"

const __float128 = "__ieee128"

const __powerpc64__ = 1

const __powerpc__ = 1
