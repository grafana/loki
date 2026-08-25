// Code generated for darwin/amd64 by 'generator -mlong-double-64 --package-name libsqlite3 --prefix-enumerator=_ --prefix-external=x_ --prefix-field=F --prefix-static-internal=_ --prefix-static-none=_ --prefix-tagged-enum=_ --prefix-tagged-struct=T --prefix-tagged-union=T --prefix-typename=T --prefix-undefined=_ -ignore-unsupported-alignment -ignore-link-errors -import=sync -DHAVE_USLEEP -DLONGDOUBLE_TYPE=double -DNDEBUG -DSQLITE_DEFAULT_MEMSTATUS=0 -DSQLITE_DISABLE_INTRINSIC -DSQLITE_ENABLE_COLUMN_METADATA -DSQLITE_ENABLE_DBPAGE_VTAB -DSQLITE_ENABLE_DBSTAT_VTAB -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_GEOPOLY -DSQLITE_ENABLE_JSON1 -DSQLITE_ENABLE_MATH_FUNCTIONS -DSQLITE_ENABLE_MEMORY_MANAGEMENT -DSQLITE_ENABLE_OFFSET_SQL_FUNC -DSQLITE_ENABLE_PREUPDATE_HOOK -DSQLITE_ENABLE_RBU -DSQLITE_ENABLE_RTREE -DSQLITE_ENABLE_SESSION -DSQLITE_ENABLE_SNAPSHOT -DSQLITE_ENABLE_STAT4 -DSQLITE_ENABLE_UNLOCK_NOTIFY -DSQLITE_HAVE_ZLIB=1 -DSQLITE_LIKE_DOESNT_MATCH_BLOBS -DSQLITE_SOUNDEX -DSQLITE_THREADSAFE=1 -DSQLITE_WITHOUT_ZONEMALLOC -D_LARGEFILE64_SOURCE -I /Users/jnml/src/modernc.org/builder/.exclude/modernc.org/libc/include/darwin/amd64 -I /Users/jnml/src/modernc.org/builder/.exclude/modernc.org/libz/include/darwin/amd64 -I /Users/jnml/src/modernc.org/builder/.exclude/modernc.org/libtcl8.6/include/darwin/amd64 -extended-errors -o sqlite3.go sqlite3.c -DSQLITE_MUTEX_NOOP -DSQLITE_OS_UNIX=1 -eval-all-macros', DO NOT EDIT.

//go:build darwin && amd64

package sqlite3

const FP_CHOP = 3

const FP_PREC_24B = 0

const FP_PREC_53B = 2

const FP_PREC_64B = 3

const FP_RND_DOWN = 1

const FP_RND_NEAR = 0

const FP_RND_UP = 2

const FP_STATE_BYTES = 512

const MAXPHYS = 131072

const MAXPHYSIO = 131072

const NMBCLUSTERS = 512

const TARGET_ABI_USES_IOS_VALUES = 0

const TARGET_CPU_ARM64 = 0

const TARGET_CPU_X86_64 = 1

const TARGET_OS_ARROW = 0

type Tboolean_t = uint32

type Tstat64 = struct {
	Fst_dev           Tdev_t
	Fst_mode          Tmode_t
	Fst_nlink         Tnlink_t
	Fst_ino           t__darwin_ino64_t
	Fst_uid           Tuid_t
	Fst_gid           Tgid_t
	Fst_rdev          Tdev_t
	Fst_atimespec     Ttimespec
	Fst_mtimespec     Ttimespec
	Fst_ctimespec     Ttimespec
	Fst_birthtimespec Ttimespec
	Fst_size          Toff_t
	Fst_blocks        Tblkcnt_t
	Fst_blksize       Tblksize_t
	Fst_flags         t__uint32_t
	Fst_gen           t__uint32_t
	Fst_lspare        t__int32_t
	Fst_qspare        [2]t__int64_t
}

type Tstatfs64 = struct {
	Ff_bsize       Tuint32_t
	Ff_iosize      Tint32_t
	Ff_blocks      Tuint64_t
	Ff_bfree       Tuint64_t
	Ff_bavail      Tuint64_t
	Ff_files       Tuint64_t
	Ff_ffree       Tuint64_t
	Ff_fsid        Tfsid_t
	Ff_owner       Tuid_t
	Ff_type        Tuint32_t
	Ff_flags       Tuint32_t
	Ff_fssubtype   Tuint32_t
	Ff_fstypename  [16]int8
	Ff_mntonname   [1024]int8
	Ff_mntfromname [1024]int8
	Ff_flags_ext   Tuint32_t
	Ff_reserved    [7]Tuint32_t
}

const _I386_SIGNAL_H_ = 1

const _X86_INSTRUCTION_STATE_CACHELINE_SIZE = 64

const _X86_INSTRUCTION_STATE_MAX_INSN_BYTES = 2380

const __DARWIN_ONLY_64_BIT_INO_T = 0

const __DARWIN_ONLY_VERS_1050 = 0

const __DARWIN_SUF_1050 = "$1050"

const __DARWIN_SUF_64_BIT_INO_T = "$INODE64"

const __LASTBRANCH_MAX = 32

const __SSE3__ = 1

const __SSE4_1__ = 1

const __SSSE3__ = 1

const __core2 = 1

const __core2__ = 1

const __tune_core2__ = 1

type stat64 = Tstat64

type statfs64 = Tstatfs64

type t__darwin_fp_control = struct {
	F__ccgo0 uint16
}

type t__darwin_fp_control_t = struct {
	F__ccgo0 uint16
}

type t__darwin_fp_status = struct {
	F__ccgo0 uint16
}

type t__darwin_fp_status_t = struct {
	F__ccgo0 uint16
}

type t__darwin_i386_avx512_state = struct {
	F__fpu_reserved  [2]int32
	F__fpu_fcw       t__darwin_fp_control
	F__fpu_fsw       t__darwin_fp_status
	F__fpu_ftw       t__uint8_t
	F__fpu_rsrv1     t__uint8_t
	F__fpu_fop       t__uint16_t
	F__fpu_ip        t__uint32_t
	F__fpu_cs        t__uint16_t
	F__fpu_rsrv2     t__uint16_t
	F__fpu_dp        t__uint32_t
	F__fpu_ds        t__uint16_t
	F__fpu_rsrv3     t__uint16_t
	F__fpu_mxcsr     t__uint32_t
	F__fpu_mxcsrmask t__uint32_t
	F__fpu_stmm0     t__darwin_mmst_reg
	F__fpu_stmm1     t__darwin_mmst_reg
	F__fpu_stmm2     t__darwin_mmst_reg
	F__fpu_stmm3     t__darwin_mmst_reg
	F__fpu_stmm4     t__darwin_mmst_reg
	F__fpu_stmm5     t__darwin_mmst_reg
	F__fpu_stmm6     t__darwin_mmst_reg
	F__fpu_stmm7     t__darwin_mmst_reg
	F__fpu_xmm0      t__darwin_xmm_reg
	F__fpu_xmm1      t__darwin_xmm_reg
	F__fpu_xmm2      t__darwin_xmm_reg
	F__fpu_xmm3      t__darwin_xmm_reg
	F__fpu_xmm4      t__darwin_xmm_reg
	F__fpu_xmm5      t__darwin_xmm_reg
	F__fpu_xmm6      t__darwin_xmm_reg
	F__fpu_xmm7      t__darwin_xmm_reg
	F__fpu_rsrv4     [224]int8
	F__fpu_reserved1 int32
	F__avx_reserved1 [64]int8
	F__fpu_ymmh0     t__darwin_xmm_reg
	F__fpu_ymmh1     t__darwin_xmm_reg
	F__fpu_ymmh2     t__darwin_xmm_reg
	F__fpu_ymmh3     t__darwin_xmm_reg
	F__fpu_ymmh4     t__darwin_xmm_reg
	F__fpu_ymmh5     t__darwin_xmm_reg
	F__fpu_ymmh6     t__darwin_xmm_reg
	F__fpu_ymmh7     t__darwin_xmm_reg
	F__fpu_k0        t__darwin_opmask_reg
	F__fpu_k1        t__darwin_opmask_reg
	F__fpu_k2        t__darwin_opmask_reg
	F__fpu_k3        t__darwin_opmask_reg
	F__fpu_k4        t__darwin_opmask_reg
	F__fpu_k5        t__darwin_opmask_reg
	F__fpu_k6        t__darwin_opmask_reg
	F__fpu_k7        t__darwin_opmask_reg
	F__fpu_zmmh0     t__darwin_ymm_reg
	F__fpu_zmmh1     t__darwin_ymm_reg
	F__fpu_zmmh2     t__darwin_ymm_reg
	F__fpu_zmmh3     t__darwin_ymm_reg
	F__fpu_zmmh4     t__darwin_ymm_reg
	F__fpu_zmmh5     t__darwin_ymm_reg
	F__fpu_zmmh6     t__darwin_ymm_reg
	F__fpu_zmmh7     t__darwin_ymm_reg
}

type t__darwin_i386_avx_state = struct {
	F__fpu_reserved  [2]int32
	F__fpu_fcw       t__darwin_fp_control
	F__fpu_fsw       t__darwin_fp_status
	F__fpu_ftw       t__uint8_t
	F__fpu_rsrv1     t__uint8_t
	F__fpu_fop       t__uint16_t
	F__fpu_ip        t__uint32_t
	F__fpu_cs        t__uint16_t
	F__fpu_rsrv2     t__uint16_t
	F__fpu_dp        t__uint32_t
	F__fpu_ds        t__uint16_t
	F__fpu_rsrv3     t__uint16_t
	F__fpu_mxcsr     t__uint32_t
	F__fpu_mxcsrmask t__uint32_t
	F__fpu_stmm0     t__darwin_mmst_reg
	F__fpu_stmm1     t__darwin_mmst_reg
	F__fpu_stmm2     t__darwin_mmst_reg
	F__fpu_stmm3     t__darwin_mmst_reg
	F__fpu_stmm4     t__darwin_mmst_reg
	F__fpu_stmm5     t__darwin_mmst_reg
	F__fpu_stmm6     t__darwin_mmst_reg
	F__fpu_stmm7     t__darwin_mmst_reg
	F__fpu_xmm0      t__darwin_xmm_reg
	F__fpu_xmm1      t__darwin_xmm_reg
	F__fpu_xmm2      t__darwin_xmm_reg
	F__fpu_xmm3      t__darwin_xmm_reg
	F__fpu_xmm4      t__darwin_xmm_reg
	F__fpu_xmm5      t__darwin_xmm_reg
	F__fpu_xmm6      t__darwin_xmm_reg
	F__fpu_xmm7      t__darwin_xmm_reg
	F__fpu_rsrv4     [224]int8
	F__fpu_reserved1 int32
	F__avx_reserved1 [64]int8
	F__fpu_ymmh0     t__darwin_xmm_reg
	F__fpu_ymmh1     t__darwin_xmm_reg
	F__fpu_ymmh2     t__darwin_xmm_reg
	F__fpu_ymmh3     t__darwin_xmm_reg
	F__fpu_ymmh4     t__darwin_xmm_reg
	F__fpu_ymmh5     t__darwin_xmm_reg
	F__fpu_ymmh6     t__darwin_xmm_reg
	F__fpu_ymmh7     t__darwin_xmm_reg
}

type t__darwin_i386_exception_state = struct {
	F__trapno     t__uint16_t
	F__cpu        t__uint16_t
	F__err        t__uint32_t
	F__faultvaddr t__uint32_t
}

type t__darwin_i386_float_state = struct {
	F__fpu_reserved  [2]int32
	F__fpu_fcw       t__darwin_fp_control
	F__fpu_fsw       t__darwin_fp_status
	F__fpu_ftw       t__uint8_t
	F__fpu_rsrv1     t__uint8_t
	F__fpu_fop       t__uint16_t
	F__fpu_ip        t__uint32_t
	F__fpu_cs        t__uint16_t
	F__fpu_rsrv2     t__uint16_t
	F__fpu_dp        t__uint32_t
	F__fpu_ds        t__uint16_t
	F__fpu_rsrv3     t__uint16_t
	F__fpu_mxcsr     t__uint32_t
	F__fpu_mxcsrmask t__uint32_t
	F__fpu_stmm0     t__darwin_mmst_reg
	F__fpu_stmm1     t__darwin_mmst_reg
	F__fpu_stmm2     t__darwin_mmst_reg
	F__fpu_stmm3     t__darwin_mmst_reg
	F__fpu_stmm4     t__darwin_mmst_reg
	F__fpu_stmm5     t__darwin_mmst_reg
	F__fpu_stmm6     t__darwin_mmst_reg
	F__fpu_stmm7     t__darwin_mmst_reg
	F__fpu_xmm0      t__darwin_xmm_reg
	F__fpu_xmm1      t__darwin_xmm_reg
	F__fpu_xmm2      t__darwin_xmm_reg
	F__fpu_xmm3      t__darwin_xmm_reg
	F__fpu_xmm4      t__darwin_xmm_reg
	F__fpu_xmm5      t__darwin_xmm_reg
	F__fpu_xmm6      t__darwin_xmm_reg
	F__fpu_xmm7      t__darwin_xmm_reg
	F__fpu_rsrv4     [224]int8
	F__fpu_reserved1 int32
}

type t__darwin_i386_thread_state = struct {
	F__eax    uint32
	F__ebx    uint32
	F__ecx    uint32
	F__edx    uint32
	F__edi    uint32
	F__esi    uint32
	F__ebp    uint32
	F__esp    uint32
	F__ss     uint32
	F__eflags uint32
	F__eip    uint32
	F__cs     uint32
	F__ds     uint32
	F__es     uint32
	F__fs     uint32
	F__gs     uint32
}

type t__darwin_mcontext32 = struct {
	F__es t__darwin_i386_exception_state
	F__ss t__darwin_i386_thread_state
	F__fs t__darwin_i386_float_state
}

type t__darwin_mcontext64 = struct {
	F__es t__darwin_x86_exception_state64
	F__ss t__darwin_x86_thread_state64
	F__fs t__darwin_x86_float_state64
}

type t__darwin_mcontext64_full = struct {
	F__es t__darwin_x86_exception_state64
	F__ss t__darwin_x86_thread_full_state64
	F__fs t__darwin_x86_float_state64
}

type t__darwin_mcontext_avx32 = struct {
	F__es t__darwin_i386_exception_state
	F__ss t__darwin_i386_thread_state
	F__fs t__darwin_i386_avx_state
}

type t__darwin_mcontext_avx512_32 = struct {
	F__es t__darwin_i386_exception_state
	F__ss t__darwin_i386_thread_state
	F__fs t__darwin_i386_avx512_state
}

type t__darwin_mcontext_avx512_64 = struct {
	F__es t__darwin_x86_exception_state64
	F__ss t__darwin_x86_thread_state64
	F__fs t__darwin_x86_avx512_state64
}

type t__darwin_mcontext_avx512_64_full = struct {
	F__es t__darwin_x86_exception_state64
	F__ss t__darwin_x86_thread_full_state64
	F__fs t__darwin_x86_avx512_state64
}

type t__darwin_mcontext_avx64 = struct {
	F__es t__darwin_x86_exception_state64
	F__ss t__darwin_x86_thread_state64
	F__fs t__darwin_x86_avx_state64
}

type t__darwin_mcontext_avx64_full = struct {
	F__es t__darwin_x86_exception_state64
	F__ss t__darwin_x86_thread_full_state64
	F__fs t__darwin_x86_avx_state64
}

type t__darwin_mmst_reg = struct {
	F__mmst_reg  [10]int8
	F__mmst_rsrv [6]int8
}

type t__darwin_opmask_reg = struct {
	F__opmask_reg [8]int8
}

type t__darwin_x86_avx512_state64 = struct {
	F__fpu_reserved  [2]int32
	F__fpu_fcw       t__darwin_fp_control
	F__fpu_fsw       t__darwin_fp_status
	F__fpu_ftw       t__uint8_t
	F__fpu_rsrv1     t__uint8_t
	F__fpu_fop       t__uint16_t
	F__fpu_ip        t__uint32_t
	F__fpu_cs        t__uint16_t
	F__fpu_rsrv2     t__uint16_t
	F__fpu_dp        t__uint32_t
	F__fpu_ds        t__uint16_t
	F__fpu_rsrv3     t__uint16_t
	F__fpu_mxcsr     t__uint32_t
	F__fpu_mxcsrmask t__uint32_t
	F__fpu_stmm0     t__darwin_mmst_reg
	F__fpu_stmm1     t__darwin_mmst_reg
	F__fpu_stmm2     t__darwin_mmst_reg
	F__fpu_stmm3     t__darwin_mmst_reg
	F__fpu_stmm4     t__darwin_mmst_reg
	F__fpu_stmm5     t__darwin_mmst_reg
	F__fpu_stmm6     t__darwin_mmst_reg
	F__fpu_stmm7     t__darwin_mmst_reg
	F__fpu_xmm0      t__darwin_xmm_reg
	F__fpu_xmm1      t__darwin_xmm_reg
	F__fpu_xmm2      t__darwin_xmm_reg
	F__fpu_xmm3      t__darwin_xmm_reg
	F__fpu_xmm4      t__darwin_xmm_reg
	F__fpu_xmm5      t__darwin_xmm_reg
	F__fpu_xmm6      t__darwin_xmm_reg
	F__fpu_xmm7      t__darwin_xmm_reg
	F__fpu_xmm8      t__darwin_xmm_reg
	F__fpu_xmm9      t__darwin_xmm_reg
	F__fpu_xmm10     t__darwin_xmm_reg
	F__fpu_xmm11     t__darwin_xmm_reg
	F__fpu_xmm12     t__darwin_xmm_reg
	F__fpu_xmm13     t__darwin_xmm_reg
	F__fpu_xmm14     t__darwin_xmm_reg
	F__fpu_xmm15     t__darwin_xmm_reg
	F__fpu_rsrv4     [96]int8
	F__fpu_reserved1 int32
	F__avx_reserved1 [64]int8
	F__fpu_ymmh0     t__darwin_xmm_reg
	F__fpu_ymmh1     t__darwin_xmm_reg
	F__fpu_ymmh2     t__darwin_xmm_reg
	F__fpu_ymmh3     t__darwin_xmm_reg
	F__fpu_ymmh4     t__darwin_xmm_reg
	F__fpu_ymmh5     t__darwin_xmm_reg
	F__fpu_ymmh6     t__darwin_xmm_reg
	F__fpu_ymmh7     t__darwin_xmm_reg
	F__fpu_ymmh8     t__darwin_xmm_reg
	F__fpu_ymmh9     t__darwin_xmm_reg
	F__fpu_ymmh10    t__darwin_xmm_reg
	F__fpu_ymmh11    t__darwin_xmm_reg
	F__fpu_ymmh12    t__darwin_xmm_reg
	F__fpu_ymmh13    t__darwin_xmm_reg
	F__fpu_ymmh14    t__darwin_xmm_reg
	F__fpu_ymmh15    t__darwin_xmm_reg
	F__fpu_k0        t__darwin_opmask_reg
	F__fpu_k1        t__darwin_opmask_reg
	F__fpu_k2        t__darwin_opmask_reg
	F__fpu_k3        t__darwin_opmask_reg
	F__fpu_k4        t__darwin_opmask_reg
	F__fpu_k5        t__darwin_opmask_reg
	F__fpu_k6        t__darwin_opmask_reg
	F__fpu_k7        t__darwin_opmask_reg
	F__fpu_zmmh0     t__darwin_ymm_reg
	F__fpu_zmmh1     t__darwin_ymm_reg
	F__fpu_zmmh2     t__darwin_ymm_reg
	F__fpu_zmmh3     t__darwin_ymm_reg
	F__fpu_zmmh4     t__darwin_ymm_reg
	F__fpu_zmmh5     t__darwin_ymm_reg
	F__fpu_zmmh6     t__darwin_ymm_reg
	F__fpu_zmmh7     t__darwin_ymm_reg
	F__fpu_zmmh8     t__darwin_ymm_reg
	F__fpu_zmmh9     t__darwin_ymm_reg
	F__fpu_zmmh10    t__darwin_ymm_reg
	F__fpu_zmmh11    t__darwin_ymm_reg
	F__fpu_zmmh12    t__darwin_ymm_reg
	F__fpu_zmmh13    t__darwin_ymm_reg
	F__fpu_zmmh14    t__darwin_ymm_reg
	F__fpu_zmmh15    t__darwin_ymm_reg
	F__fpu_zmm16     t__darwin_zmm_reg
	F__fpu_zmm17     t__darwin_zmm_reg
	F__fpu_zmm18     t__darwin_zmm_reg
	F__fpu_zmm19     t__darwin_zmm_reg
	F__fpu_zmm20     t__darwin_zmm_reg
	F__fpu_zmm21     t__darwin_zmm_reg
	F__fpu_zmm22     t__darwin_zmm_reg
	F__fpu_zmm23     t__darwin_zmm_reg
	F__fpu_zmm24     t__darwin_zmm_reg
	F__fpu_zmm25     t__darwin_zmm_reg
	F__fpu_zmm26     t__darwin_zmm_reg
	F__fpu_zmm27     t__darwin_zmm_reg
	F__fpu_zmm28     t__darwin_zmm_reg
	F__fpu_zmm29     t__darwin_zmm_reg
	F__fpu_zmm30     t__darwin_zmm_reg
	F__fpu_zmm31     t__darwin_zmm_reg
}

type t__darwin_x86_avx_state64 = struct {
	F__fpu_reserved  [2]int32
	F__fpu_fcw       t__darwin_fp_control
	F__fpu_fsw       t__darwin_fp_status
	F__fpu_ftw       t__uint8_t
	F__fpu_rsrv1     t__uint8_t
	F__fpu_fop       t__uint16_t
	F__fpu_ip        t__uint32_t
	F__fpu_cs        t__uint16_t
	F__fpu_rsrv2     t__uint16_t
	F__fpu_dp        t__uint32_t
	F__fpu_ds        t__uint16_t
	F__fpu_rsrv3     t__uint16_t
	F__fpu_mxcsr     t__uint32_t
	F__fpu_mxcsrmask t__uint32_t
	F__fpu_stmm0     t__darwin_mmst_reg
	F__fpu_stmm1     t__darwin_mmst_reg
	F__fpu_stmm2     t__darwin_mmst_reg
	F__fpu_stmm3     t__darwin_mmst_reg
	F__fpu_stmm4     t__darwin_mmst_reg
	F__fpu_stmm5     t__darwin_mmst_reg
	F__fpu_stmm6     t__darwin_mmst_reg
	F__fpu_stmm7     t__darwin_mmst_reg
	F__fpu_xmm0      t__darwin_xmm_reg
	F__fpu_xmm1      t__darwin_xmm_reg
	F__fpu_xmm2      t__darwin_xmm_reg
	F__fpu_xmm3      t__darwin_xmm_reg
	F__fpu_xmm4      t__darwin_xmm_reg
	F__fpu_xmm5      t__darwin_xmm_reg
	F__fpu_xmm6      t__darwin_xmm_reg
	F__fpu_xmm7      t__darwin_xmm_reg
	F__fpu_xmm8      t__darwin_xmm_reg
	F__fpu_xmm9      t__darwin_xmm_reg
	F__fpu_xmm10     t__darwin_xmm_reg
	F__fpu_xmm11     t__darwin_xmm_reg
	F__fpu_xmm12     t__darwin_xmm_reg
	F__fpu_xmm13     t__darwin_xmm_reg
	F__fpu_xmm14     t__darwin_xmm_reg
	F__fpu_xmm15     t__darwin_xmm_reg
	F__fpu_rsrv4     [96]int8
	F__fpu_reserved1 int32
	F__avx_reserved1 [64]int8
	F__fpu_ymmh0     t__darwin_xmm_reg
	F__fpu_ymmh1     t__darwin_xmm_reg
	F__fpu_ymmh2     t__darwin_xmm_reg
	F__fpu_ymmh3     t__darwin_xmm_reg
	F__fpu_ymmh4     t__darwin_xmm_reg
	F__fpu_ymmh5     t__darwin_xmm_reg
	F__fpu_ymmh6     t__darwin_xmm_reg
	F__fpu_ymmh7     t__darwin_xmm_reg
	F__fpu_ymmh8     t__darwin_xmm_reg
	F__fpu_ymmh9     t__darwin_xmm_reg
	F__fpu_ymmh10    t__darwin_xmm_reg
	F__fpu_ymmh11    t__darwin_xmm_reg
	F__fpu_ymmh12    t__darwin_xmm_reg
	F__fpu_ymmh13    t__darwin_xmm_reg
	F__fpu_ymmh14    t__darwin_xmm_reg
	F__fpu_ymmh15    t__darwin_xmm_reg
}

type t__darwin_x86_cpmu_state64 = struct {
	F__ctrs [16]t__uint64_t
}

type t__darwin_x86_debug_state32 = struct {
	F__dr0 uint32
	F__dr1 uint32
	F__dr2 uint32
	F__dr3 uint32
	F__dr4 uint32
	F__dr5 uint32
	F__dr6 uint32
	F__dr7 uint32
}

type t__darwin_x86_debug_state64 = struct {
	F__dr0 t__uint64_t
	F__dr1 t__uint64_t
	F__dr2 t__uint64_t
	F__dr3 t__uint64_t
	F__dr4 t__uint64_t
	F__dr5 t__uint64_t
	F__dr6 t__uint64_t
	F__dr7 t__uint64_t
}

type t__darwin_x86_exception_state64 = struct {
	F__trapno     t__uint16_t
	F__cpu        t__uint16_t
	F__err        t__uint32_t
	F__faultvaddr t__uint64_t
}

type t__darwin_x86_float_state64 = struct {
	F__fpu_reserved  [2]int32
	F__fpu_fcw       t__darwin_fp_control
	F__fpu_fsw       t__darwin_fp_status
	F__fpu_ftw       t__uint8_t
	F__fpu_rsrv1     t__uint8_t
	F__fpu_fop       t__uint16_t
	F__fpu_ip        t__uint32_t
	F__fpu_cs        t__uint16_t
	F__fpu_rsrv2     t__uint16_t
	F__fpu_dp        t__uint32_t
	F__fpu_ds        t__uint16_t
	F__fpu_rsrv3     t__uint16_t
	F__fpu_mxcsr     t__uint32_t
	F__fpu_mxcsrmask t__uint32_t
	F__fpu_stmm0     t__darwin_mmst_reg
	F__fpu_stmm1     t__darwin_mmst_reg
	F__fpu_stmm2     t__darwin_mmst_reg
	F__fpu_stmm3     t__darwin_mmst_reg
	F__fpu_stmm4     t__darwin_mmst_reg
	F__fpu_stmm5     t__darwin_mmst_reg
	F__fpu_stmm6     t__darwin_mmst_reg
	F__fpu_stmm7     t__darwin_mmst_reg
	F__fpu_xmm0      t__darwin_xmm_reg
	F__fpu_xmm1      t__darwin_xmm_reg
	F__fpu_xmm2      t__darwin_xmm_reg
	F__fpu_xmm3      t__darwin_xmm_reg
	F__fpu_xmm4      t__darwin_xmm_reg
	F__fpu_xmm5      t__darwin_xmm_reg
	F__fpu_xmm6      t__darwin_xmm_reg
	F__fpu_xmm7      t__darwin_xmm_reg
	F__fpu_xmm8      t__darwin_xmm_reg
	F__fpu_xmm9      t__darwin_xmm_reg
	F__fpu_xmm10     t__darwin_xmm_reg
	F__fpu_xmm11     t__darwin_xmm_reg
	F__fpu_xmm12     t__darwin_xmm_reg
	F__fpu_xmm13     t__darwin_xmm_reg
	F__fpu_xmm14     t__darwin_xmm_reg
	F__fpu_xmm15     t__darwin_xmm_reg
	F__fpu_rsrv4     [96]int8
	F__fpu_reserved1 int32
}

type t__darwin_x86_thread_full_state64 = struct {
	F__ss64   t__darwin_x86_thread_state64
	F__ds     t__uint64_t
	F__es     t__uint64_t
	F__ss     t__uint64_t
	F__gsbase t__uint64_t
}

type t__darwin_x86_thread_state64 = struct {
	F__rax    t__uint64_t
	F__rbx    t__uint64_t
	F__rcx    t__uint64_t
	F__rdx    t__uint64_t
	F__rdi    t__uint64_t
	F__rsi    t__uint64_t
	F__rbp    t__uint64_t
	F__rsp    t__uint64_t
	F__r8     t__uint64_t
	F__r9     t__uint64_t
	F__r10    t__uint64_t
	F__r11    t__uint64_t
	F__r12    t__uint64_t
	F__r13    t__uint64_t
	F__r14    t__uint64_t
	F__r15    t__uint64_t
	F__rip    t__uint64_t
	F__rflags t__uint64_t
	F__cs     t__uint64_t
	F__fs     t__uint64_t
	F__gs     t__uint64_t
}

type t__darwin_xmm_reg = struct {
	F__xmm_reg [16]int8
}

type t__darwin_ymm_reg = struct {
	F__ymm_reg [32]int8
}

type t__darwin_zmm_reg = struct {
	F__zmm_reg [64]int8
}

type t__last_branch_record = struct {
	F__from_ip t__uint64_t
	F__to_ip   t__uint64_t
	F__ccgo16  uint32
}

type t__last_branch_state = struct {
	F__lbr_count int32
	F__ccgo4     uint32
	F__lbrs      [32]t__last_branch_record
}

type t__x86_instruction_state = struct {
	F__insn_stream_valid_bytes int32
	F__insn_offset             int32
	F__out_of_synch            int32
	F__insn_bytes              [2380]t__uint8_t
	F__insn_cacheline          [64]t__uint8_t
}

type t__x86_pagein_state = struct {
	F__pagein_error int32
}
