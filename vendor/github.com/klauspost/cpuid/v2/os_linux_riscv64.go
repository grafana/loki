// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

package cpuid

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

const __NR_riscv_hwprobe = 258

type riscvHWProbePair struct {
	key   int64
	value uint64
}

// Keys from linux/include/uapi/asm/hwprobe.h
const (
	riscv_hwprobe_key_mvendorid         = 0
	riscv_hwprobe_key_marchid           = 1
	riscv_hwprobe_key_mimpid            = 2
	riscv_hwprobe_key_base_behavior     = 3
	riscv_hwprobe_key_ima_ext_0         = 4
	riscv_hwprobe_key_cpuperf_0         = 5
	riscv_hwprobe_key_zicbom_block_size = 12
)

// Bits from linux/arch/riscv/include/uapi/asm/hwprobe.h
const (
	riscv_hwprobe_ima_fd          = 1 << 0
	riscv_hwprobe_ima_c           = 1 << 1
	riscv_hwprobe_ima_v           = 1 << 2
	riscv_hwprobe_ext_zba         = 1 << 3
	riscv_hwprobe_ext_zbb         = 1 << 4
	riscv_hwprobe_ext_zbs         = 1 << 5
	riscv_hwprobe_ext_zicboz      = 1 << 6
	riscv_hwprobe_ext_zbc         = 1 << 7
	riscv_hwprobe_ext_zbkb        = 1 << 8
	riscv_hwprobe_ext_zbkc        = 1 << 9
	riscv_hwprobe_ext_zbkx        = 1 << 10
	riscv_hwprobe_ext_zknd        = 1 << 11
	riscv_hwprobe_ext_zkne        = 1 << 12
	riscv_hwprobe_ext_zknh        = 1 << 13
	riscv_hwprobe_ext_zksed       = 1 << 14
	riscv_hwprobe_ext_zksh        = 1 << 15
	riscv_hwprobe_ext_zkt         = 1 << 16
	riscv_hwprobe_ext_zvbb        = 1 << 17
	riscv_hwprobe_ext_zvbc        = 1 << 18
	riscv_hwprobe_ext_zvkb        = 1 << 19
	riscv_hwprobe_ext_zvkg        = 1 << 20
	riscv_hwprobe_ext_zvkned      = 1 << 21
	riscv_hwprobe_ext_zvknha      = 1 << 22
	riscv_hwprobe_ext_zvknhb      = 1 << 23
	riscv_hwprobe_ext_zvksed      = 1 << 24
	riscv_hwprobe_ext_zvksh       = 1 << 25
	riscv_hwprobe_ext_zvkt        = 1 << 26
	riscv_hwprobe_ext_zfh         = 1 << 27
	riscv_hwprobe_ext_zfhmin      = 1 << 28
	riscv_hwprobe_ext_zihintntl   = 1 << 29
	riscv_hwprobe_ext_zvfh        = 1 << 30
	riscv_hwprobe_ext_zvfhmin     = 1 << 31
	riscv_hwprobe_ext_zfa         = 1 << 32
	riscv_hwprobe_ext_ztso        = 1 << 33
	riscv_hwprobe_ext_zacas       = 1 << 34
	riscv_hwprobe_ext_zicond      = 1 << 35
	riscv_hwprobe_ext_zihintpause = 1 << 36
	riscv_hwprobe_ext_zicbom      = 1 << 55
	riscv_hwprobe_ext_zicbop      = 1 << 60
)

func riscvHWProbe(pairs []riscvHWProbePair) int64 {
	if len(pairs) == 0 {
		return -1
	}
	ret, _, _ := unix.Syscall6(__NR_riscv_hwprobe,
		uintptr(unsafe.Pointer(&pairs[0])),
		uintptr(len(pairs)),
		0, 0, 0, 0)
	return int64(ret)
}

func detectOS(c *CPUInfo) bool {
	c.LogicalCores = runtime.NumCPU()
	c.PhysicalCores = c.LogicalCores
	c.ThreadsPerCore = 1

	pairs := []riscvHWProbePair{
		{key: riscv_hwprobe_key_mvendorid},
		{key: riscv_hwprobe_key_marchid},
		{key: riscv_hwprobe_key_mimpid},
		{key: riscv_hwprobe_key_ima_ext_0},
		{key: riscv_hwprobe_key_zicbom_block_size},
	}
	ret := riscvHWProbe(pairs)
	if ret == 0 && pairs[3].value != ^uint64(0) {
		detectFromHWProbe(c, pairs)
		if pairs[4].value != ^uint64(0) && pairs[4].value > 0 {
			c.CacheLine = int(pairs[4].value)
		}
		return true
	}

	c.CacheLine = detectCacheLine()
	return detectFromCPUInfo(c)
}

func detectFromHWProbe(c *CPUInfo, pairs []riscvHWProbePair) {
	if pairs[0].value != ^uint64(0) {
		c.VendorID = riscvVendorID(pairs[0].value)
		c.VendorString = c.VendorID.String()
	}
	if pairs[1].value != ^uint64(0) {
		c.Model = int(pairs[1].value)
	}
	if pairs[2].value != ^uint64(0) {
		c.Family = int(pairs[2].value)
	}

	imaExt := pairs[3].value

	if imaExt&riscv_hwprobe_ima_fd != 0 {
		c.featureSet.set(RV_D)
		c.featureSet.set(RV_F)
	}
	c.featureSet.setIf(imaExt&riscv_hwprobe_ima_c != 0, RV_C)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ima_v != 0, RV_V)

	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zba != 0, RV_ZBA)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zbb != 0, RV_ZBB)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zbc != 0, RV_ZBC)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zbs != 0, RV_ZBS)

	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zicbom != 0, RV_ZICBOM)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zicboz != 0, RV_ZICBOZ)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zicbop != 0, RV_ZICBOP)

	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zicond != 0, RV_ZICOND)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zihintpause != 0, RV_ZIHINTPAUSE)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zfa != 0, RV_ZFA)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zfh != 0, RV_ZFH)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zfhmin != 0, RV_ZFHMIN)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_ztso != 0, RV_ZTSO)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zacas != 0, RV_ZACAS)

	// Scalar cryptography
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zbkb != 0, RV_ZBKB)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zbkc != 0, RV_ZBKC)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zbkx != 0, RV_ZBKX)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zknd != 0, RV_ZKND)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zkne != 0, RV_ZKNE)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zknh != 0, RV_ZKNH)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zksed != 0, RV_ZKSED)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zksh != 0, RV_ZKSH)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zkt != 0, RV_ZKT)

	// Vector cryptography
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvbb != 0, RV_ZVBB)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvbc != 0, RV_ZVBC)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvkb != 0, RV_ZVKB)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvkg != 0, RV_ZVKG)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvkned != 0, RV_ZVKNED)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvknha != 0, RV_ZVKNHA)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvknhb != 0, RV_ZVKNHB)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvksed != 0, RV_ZVKSED)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvksh != 0, RV_ZVKSH)
	c.featureSet.setIf(imaExt&riscv_hwprobe_ext_zvkt != 0, RV_ZVKT)

	// Crypto suites (combined from individual features)
	c.featureSet.setIf(c.featureSet.hasSetP(rvZKNFeatures), RV_ZKN)
	c.featureSet.setIf(c.featureSet.hasSetP(rvZKSFeatures), RV_ZKS)
	c.featureSet.setIf(c.featureSet.hasSetP(rvZVKNFeatures), RV_ZVKNG)
	c.featureSet.setIf(c.featureSet.hasSetP(rvZVKSFeatures), RV_ZVKSG)

	// Every Linux-capable riscv64 core has I, M, A base.
	c.featureSet.set(RV_IMA)
}

func detectFromCPUInfo(c *CPUInfo) bool {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.SplitN(line, ":", 2)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSpace(fields[0])
		value := strings.TrimSpace(fields[1])

		switch key {
		case "isa":
			parseISAString(c, value)
		case "mvendorid":
			if vid, err := strconv.ParseUint(value, 0, 64); err == nil {
				c.VendorID = riscvVendorID(vid)
				c.VendorString = c.VendorID.String()
			}
		case "uarch":
			c.BrandName = value
		}
	}
	return scanner.Err() == nil
}

func detectCacheLine() int {
	data, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cache/index0/coherency_line_size")
	if err != nil {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && n > 0 {
		return n
	}
	return 0
}
