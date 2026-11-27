// Code generated for darwin/arm64 by 'generator -D_GCC_NULLPTR_T -D_Float16=short -D__bf16=short --package-name libsqlite3 --prefix-enumerator=_ --prefix-external=x_ --prefix-field=F --prefix-static-internal=_ --prefix-static-none=_ --prefix-tagged-enum=_ --prefix-tagged-struct=T --prefix-tagged-union=T --prefix-typename=T --prefix-undefined=_ -ignore-unsupported-alignment -ignore-link-errors -import=sync -DHAVE_USLEEP -DLONGDOUBLE_TYPE=double -DNDEBUG -DSQLITE_DEFAULT_MEMSTATUS=0 -DSQLITE_DISABLE_INTRINSIC -DSQLITE_ENABLE_COLUMN_METADATA -DSQLITE_ENABLE_DBPAGE_VTAB -DSQLITE_ENABLE_DBSTAT_VTAB -DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_GEOPOLY -DSQLITE_ENABLE_JSON1 -DSQLITE_ENABLE_MATH_FUNCTIONS -DSQLITE_ENABLE_MEMORY_MANAGEMENT -DSQLITE_ENABLE_OFFSET_SQL_FUNC -DSQLITE_ENABLE_PREUPDATE_HOOK -DSQLITE_ENABLE_RBU -DSQLITE_ENABLE_RTREE -DSQLITE_ENABLE_SESSION -DSQLITE_ENABLE_SNAPSHOT -DSQLITE_ENABLE_STAT4 -DSQLITE_ENABLE_UNLOCK_NOTIFY -DSQLITE_HAVE_ZLIB=1 -DSQLITE_LIKE_DOESNT_MATCH_BLOBS -DSQLITE_SOUNDEX -DSQLITE_THREADSAFE=1 -DSQLITE_WITHOUT_ZONEMALLOC -D_LARGEFILE64_SOURCE -I /Users/jnml/src/modernc.org/builder/.exclude/modernc.org/libc/include/darwin/arm64 -I /Users/jnml/src/modernc.org/builder/.exclude/modernc.org/libz/include/darwin/arm64 -I /Users/jnml/src/modernc.org/builder/.exclude/modernc.org/libtcl8.6/include/darwin/arm64 -extended-errors -o sqlite3.go sqlite3.c -DSQLITE_MUTEX_NOOP -DSQLITE_OS_UNIX=1 -eval-all-macros', DO NOT EDIT.

//go:build darwin && arm64

package sqlite3

type Dl_info_t = TDl_info_t

/*
 * Flags for dlopen().
 */

/*
 * Special handle values for dlsym().
 */

const FD_CLOFORK = 2

const F_DUPFD_CLOFORK = 115

const F_RDADVISEV = 116

const F_RDADVISEV_NOAGE = 1

const IFCAP_IPSEC = 65536

const IFCAP_VALID = 131071

const IOPOL_TYPE_VFS_CONTENT_PROTECTION = 6

const IOPOL_VFS_CONTENT_PROTECTION_STRICT = 2

const MACH_PORT_KERNEL_READ_ENTITLEMENT = "com.apple.private.system-task-ports.kernel.read"

const MACH_PORT_QLIMIT_KERNEL_SOFT = 4096

const MAC_OS_VERSION_26_6 = 260600

const MAC_OS_VERSION_27_0 = 270000

const MAXPHYS = 65536

const MAXPHYSIO = 65536

const NMBCLUSTERS = "CONFIG_NMBCLUSTERS"

const PTHREAD_STACK_MIN = 16384

const READ_ADVISE_RANGES_MAX = 64

const SBC_SKIP_AUTH = 64

const SO_MAX_PACING_RATE = 4406

const TARGET_ABI_USES_IOS_VALUES = 1

const TARGET_CPU_ARM64 = 1

const TARGET_CPU_X86_64 = 0

const TARGET_OS_ARROW = 1

const TARGET_OS_FIRMWARE = 0

const TARGET_OS_KERNELKIT = 0

type Tboolean_t = int32

type Tradvisoryv = struct {
	Frav_flags  uint32
	Frav_count  uint32
	Frav_ranges uintptr
}

type Trusage_info_current = struct {
	Fri_uuid                          [16]Tuint8_t
	Fri_user_time                     Tuint64_t
	Fri_system_time                   Tuint64_t
	Fri_pkg_idle_wkups                Tuint64_t
	Fri_interrupt_wkups               Tuint64_t
	Fri_pageins                       Tuint64_t
	Fri_wired_size                    Tuint64_t
	Fri_resident_size                 Tuint64_t
	Fri_phys_footprint                Tuint64_t
	Fri_proc_start_abstime            Tuint64_t
	Fri_proc_exit_abstime             Tuint64_t
	Fri_child_user_time               Tuint64_t
	Fri_child_system_time             Tuint64_t
	Fri_child_pkg_idle_wkups          Tuint64_t
	Fri_child_interrupt_wkups         Tuint64_t
	Fri_child_pageins                 Tuint64_t
	Fri_child_elapsed_abstime         Tuint64_t
	Fri_diskio_bytesread              Tuint64_t
	Fri_diskio_byteswritten           Tuint64_t
	Fri_cpu_time_qos_default          Tuint64_t
	Fri_cpu_time_qos_maintenance      Tuint64_t
	Fri_cpu_time_qos_background       Tuint64_t
	Fri_cpu_time_qos_utility          Tuint64_t
	Fri_cpu_time_qos_legacy           Tuint64_t
	Fri_cpu_time_qos_user_initiated   Tuint64_t
	Fri_cpu_time_qos_user_interactive Tuint64_t
	Fri_billed_system_time            Tuint64_t
	Fri_serviced_system_time          Tuint64_t
	Fri_logical_writes                Tuint64_t
	Fri_lifetime_max_phys_footprint   Tuint64_t
	Fri_instructions                  Tuint64_t
	Fri_cycles                        Tuint64_t
	Fri_billed_energy                 Tuint64_t
	Fri_serviced_energy               Tuint64_t
	Fri_interval_max_phys_footprint   Tuint64_t
	Fri_runnable_time                 Tuint64_t
	Fri_flags                         Tuint64_t
	Fri_user_ptime                    Tuint64_t
	Fri_system_ptime                  Tuint64_t
	Fri_pinstructions                 Tuint64_t
	Fri_pcycles                       Tuint64_t
	Fri_energy_nj                     Tuint64_t
	Fri_penergy_nj                    Tuint64_t
	Fri_secure_time_in_system         Tuint64_t
	Fri_secure_ptime_in_system        Tuint64_t
	Fri_neural_footprint              Tuint64_t
	Fri_lifetime_max_neural_footprint Tuint64_t
	Fri_interval_max_neural_footprint Tuint64_t
	Fri_conclave_footprint            Tuint64_t
	Fri_page_wait_time_mach           Tuint64_t
	Fri_page_cache_hits               Tuint64_t
	Fri_reserved                      [6]Tuint64_t
}

type Trusage_info_v6 = struct {
	Fri_uuid                          [16]Tuint8_t
	Fri_user_time                     Tuint64_t
	Fri_system_time                   Tuint64_t
	Fri_pkg_idle_wkups                Tuint64_t
	Fri_interrupt_wkups               Tuint64_t
	Fri_pageins                       Tuint64_t
	Fri_wired_size                    Tuint64_t
	Fri_resident_size                 Tuint64_t
	Fri_phys_footprint                Tuint64_t
	Fri_proc_start_abstime            Tuint64_t
	Fri_proc_exit_abstime             Tuint64_t
	Fri_child_user_time               Tuint64_t
	Fri_child_system_time             Tuint64_t
	Fri_child_pkg_idle_wkups          Tuint64_t
	Fri_child_interrupt_wkups         Tuint64_t
	Fri_child_pageins                 Tuint64_t
	Fri_child_elapsed_abstime         Tuint64_t
	Fri_diskio_bytesread              Tuint64_t
	Fri_diskio_byteswritten           Tuint64_t
	Fri_cpu_time_qos_default          Tuint64_t
	Fri_cpu_time_qos_maintenance      Tuint64_t
	Fri_cpu_time_qos_background       Tuint64_t
	Fri_cpu_time_qos_utility          Tuint64_t
	Fri_cpu_time_qos_legacy           Tuint64_t
	Fri_cpu_time_qos_user_initiated   Tuint64_t
	Fri_cpu_time_qos_user_interactive Tuint64_t
	Fri_billed_system_time            Tuint64_t
	Fri_serviced_system_time          Tuint64_t
	Fri_logical_writes                Tuint64_t
	Fri_lifetime_max_phys_footprint   Tuint64_t
	Fri_instructions                  Tuint64_t
	Fri_cycles                        Tuint64_t
	Fri_billed_energy                 Tuint64_t
	Fri_serviced_energy               Tuint64_t
	Fri_interval_max_phys_footprint   Tuint64_t
	Fri_runnable_time                 Tuint64_t
	Fri_flags                         Tuint64_t
	Fri_user_ptime                    Tuint64_t
	Fri_system_ptime                  Tuint64_t
	Fri_pinstructions                 Tuint64_t
	Fri_pcycles                       Tuint64_t
	Fri_energy_nj                     Tuint64_t
	Fri_penergy_nj                    Tuint64_t
	Fri_secure_time_in_system         Tuint64_t
	Fri_secure_ptime_in_system        Tuint64_t
	Fri_neural_footprint              Tuint64_t
	Fri_lifetime_max_neural_footprint Tuint64_t
	Fri_interval_max_neural_footprint Tuint64_t
	Fri_conclave_footprint            Tuint64_t
	Fri_page_wait_time_mach           Tuint64_t
	Fri_page_cache_hits               Tuint64_t
	Fri_reserved                      [6]Tuint64_t
}

type Tvm32_address_t = uint32

type Tvm32_offset_t = uint32

type Tvm32_size_t = uint32

const _ARM_SIGNAL_ = 1

const _DARWIN_FEATURE_ONLY_64_BIT_INODE = 1

const _DARWIN_FEATURE_ONLY_VERS_1050 = 1

const __AARCH64_SIMD__ = 1

const __ARM64_ARCH_8__ = 1

const __ARM_ACLE = 202420

const __ARM_ARCH_8_2__ = 1

const __ARM_ARCH_8_3__ = 1

const __ARM_ARCH_8_4__ = 1

const __ARM_ARCH_8_5__ = 1

const __ARM_FEATURE_AES = 1

const __ARM_FEATURE_ATOMICS = 1

const __ARM_FEATURE_BTI = 1

const __ARM_FEATURE_COMPLEX = 1

const __ARM_FEATURE_CRC32 = 1

const __ARM_FEATURE_CRYPTO = 1

const __ARM_FEATURE_DOTPROD = 1

const __ARM_FEATURE_FP16_FML = 1

const __ARM_FEATURE_FP16_SCALAR_ARITHMETIC = 1

const __ARM_FEATURE_FP16_VECTOR_ARITHMETIC = 1

const __ARM_FEATURE_FRINT = 1

const __ARM_FEATURE_JCVT = 1

const __ARM_FEATURE_PAUTH = 1

const __ARM_FEATURE_QRDMX = 1

const __ARM_FEATURE_RCPC = 1

const __ARM_FEATURE_SHA2 = 1

const __ARM_FEATURE_SHA3 = 1

const __ARM_FEATURE_SHA512 = 1

const __ARM_NEON_SVE_BRIDGE = 1

const __BRIDGEOS_10_5 = 100500

const __BRIDGEOS_10_6 = 100600

const __DARWIN_ONLY_64_BIT_INO_T = 1

const __DARWIN_ONLY_VERS_1050 = 1

const __DARWIN_OPAQUE_ARM_THREAD_STATE64 = 0

const __DRIVERKIT_25_6 = 250600

const __DRIVERKIT_27_0 = 270000

const __ENVIRONMENT_MAC_OS_X_VERSION_MIN_REQUIRED__ = 270000

const __ENVIRONMENT_OS_VERSION_MIN_REQUIRED__ = 270000

const __FUNCTION_MULTI_VERSIONING_SUPPORT_LEVEL = 202430

const __IPHONE_26_6 = 260600

const __IPHONE_27_0 = 270000

const __MAC_26_6 = 260600

const __MAC_27_0 = 270000

const __OBJC_BOOL_IS_BOOL = 1

const __TVOS_26_6 = 260600

const __TVOS_27_0 = 270000

const __VERSION__ = "Apple LLVM 21.0.0 (clang-2100.3.34.2)"

const __VISIONOS_26_6 = 260600

const __VISIONOS_27_0 = 270000

const __WATCHOS_26_6 = 260600

const __WATCHOS_27_0 = 270000

const __apple_build_version__ = 21000334

const __arm64 = 1

const __arm64__ = 1

const __clang_version__ = "21.0.0 (clang-2100.3.34.2)"

const _kGUARD_EXC_BOOTSTRAP_PORT_NOTIFICATION = 17

const _kGUARD_EXC_CV_INVALID_EXCEPTION_PORT = 18

const _kGUARD_EXC_MACH_EXC_THREAD_SET_STATE = 12

const _kGUARD_EXC_MOVE_WEAK_REPLY_PORT_FATAL = 13

const _kGUARD_EXC_RESTRICT_INLINE_PORT_DESCRIPTORS = 34

const _kGUARD_EXC_RESTRICT_VOUCHER_OPERATIONS = 11

const _kGUARD_EXC_RESTRICT_VOUCHER_RECIPE_SIZE = 33

type radvisoryv = Tradvisoryv

type t__arm_legacy_debug_state = struct {
	F__bvr [16]t__uint32_t
	F__bcr [16]t__uint32_t
	F__wvr [16]t__uint32_t
	F__wcr [16]t__uint32_t
}

type t__arm_pagein_state = struct {
	F__pagein_error int32
}

type t__darwin_arm_cpmu_state64 = struct {
	F__ctrs [16]t__uint64_t
}

type t__darwin_arm_debug_state32 = struct {
	F__bvr       [16]t__uint32_t
	F__bcr       [16]t__uint32_t
	F__wvr       [16]t__uint32_t
	F__wcr       [16]t__uint32_t
	F__mdscr_el1 t__uint64_t
}

type t__darwin_arm_debug_state64 = struct {
	F__bvr       [16]t__uint64_t
	F__bcr       [16]t__uint64_t
	F__wvr       [16]t__uint64_t
	F__wcr       [16]t__uint64_t
	F__mdscr_el1 t__uint64_t
}

type t__darwin_arm_exception_state = struct {
	F__exception t__uint32_t
	F__fsr       t__uint32_t
	F__far       t__uint32_t
}

type t__darwin_arm_exception_state64 = struct {
	F__far       t__uint64_t
	F__esr       t__uint32_t
	F__exception t__uint32_t
}

type t__darwin_arm_exception_state64_v2 = struct {
	F__far t__uint64_t
	F__esr t__uint64_t
}

type t__darwin_arm_neon_state = struct {
	F__ccgo_align [0]uint64
	F__v          [16][2]uint64
	F__fpsr       t__uint32_t
	F__fpcr       t__uint32_t
	F__ccgo_pad3  [8]byte
}

type t__darwin_arm_neon_state64 = struct {
	F__ccgo_align [0]uint64
	F__v          [32][2]uint64
	F__fpsr       t__uint32_t
	F__fpcr       t__uint32_t
	F__ccgo_pad3  [8]byte
}

type t__darwin_arm_sme2_state = struct {
	F__ccgo_align [0]uint32
	F__zt0        [64]int8
}

type t__darwin_arm_sme_state = struct {
	F__svcr       t__uint64_t
	F__tpidr2_el0 t__uint64_t
	F__svl_b      t__uint16_t
}

type t__darwin_arm_sme_za_state = struct {
	F__ccgo_align [0]uint32
	F__za         [4096]int8
}

type t__darwin_arm_sve_p_state = struct {
	F__ccgo_align [0]uint32
	F__p          [16][32]int8
}

type t__darwin_arm_sve_z_state = struct {
	F__ccgo_align [0]uint32
	F__z          [16][256]int8
}

type t__darwin_arm_thread_state = struct {
	F__r    [13]t__uint32_t
	F__sp   t__uint32_t
	F__lr   t__uint32_t
	F__pc   t__uint32_t
	F__cpsr t__uint32_t
}

type t__darwin_arm_thread_state64 = struct {
	F__x    [29]t__uint64_t
	F__fp   t__uint64_t
	F__lr   t__uint64_t
	F__sp   t__uint64_t
	F__pc   t__uint64_t
	F__cpsr t__uint32_t
	F__pad  t__uint32_t
}

type t__darwin_arm_vfp_state = struct {
	F__r     [64]t__uint32_t
	F__fpscr t__uint32_t
}

type t__darwin_mcontext32 = struct {
	F__es t__darwin_arm_exception_state
	F__ss t__darwin_arm_thread_state
	F__fs t__darwin_arm_vfp_state
}

type t__darwin_mcontext64 = struct {
	F__ccgo_align [0]uint64
	F__es         t__darwin_arm_exception_state64
	F__ss         t__darwin_arm_thread_state64
	F__ns         t__darwin_arm_neon_state64
}

type vm32_address_t = Tvm32_address_t

type vm32_offset_t = Tvm32_offset_t

type vm32_size_t = Tvm32_size_t
