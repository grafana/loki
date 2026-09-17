// Copyright (c) 2026 Klaus Post, released under MIT License. See LICENSE file.

package cpuid

import "strings"

func parseISAString(c *CPUInfo, isa string) {
	isa = strings.ToLower(isa)
	extMap := make(map[string]bool)
	for ext := range strings.SplitSeq(isa, "_") {
		ext = strings.TrimSpace(ext)
		if strings.HasPrefix(ext, "rv64") {
			extMap["i"] = true
			for _, ch := range ext[4:] {
				extMap[string(ch)] = true
			}
		} else if ext != "" {
			extMap[ext] = true
		}
	}

	if extMap["g"] {
		extMap["i"] = true
		extMap["m"] = true
		extMap["a"] = true
		extMap["f"] = true
		extMap["d"] = true
	}

	c.featureSet.setIf(extMap["i"] && extMap["m"] && extMap["a"], RV_IMA)
	c.featureSet.setIf(extMap["c"], RV_C)
	c.featureSet.setIf(extMap["f"], RV_F)
	c.featureSet.setIf(extMap["d"], RV_D)
	c.featureSet.setIf(extMap["v"], RV_V)
	c.featureSet.setIf(extMap["zihintpause"], RV_ZIHINTPAUSE)
	c.featureSet.setIf(extMap["zba"], RV_ZBA)
	c.featureSet.setIf(extMap["zbb"], RV_ZBB)
	c.featureSet.setIf(extMap["zbc"], RV_ZBC)
	c.featureSet.setIf(extMap["zbs"], RV_ZBS)
	c.featureSet.setIf(extMap["zicond"], RV_ZICOND)
	c.featureSet.setIf(extMap["zicbom"], RV_ZICBOM)
	c.featureSet.setIf(extMap["zicboz"], RV_ZICBOZ)
	c.featureSet.setIf(extMap["zicbop"], RV_ZICBOP)
	c.featureSet.setIf(extMap["zfa"], RV_ZFA)
	c.featureSet.setIf(extMap["zfh"], RV_ZFH)
	c.featureSet.setIf(extMap["zfhmin"], RV_ZFHMIN)
	c.featureSet.setIf(extMap["ztso"], RV_ZTSO)
	c.featureSet.setIf(extMap["zacas"], RV_ZACAS)

	// Scalar cryptography
	c.featureSet.setIf(extMap["zbkb"], RV_ZBKB)
	c.featureSet.setIf(extMap["zbkc"], RV_ZBKC)
	c.featureSet.setIf(extMap["zbkx"], RV_ZBKX)
	c.featureSet.setIf(extMap["zknd"], RV_ZKND)
	c.featureSet.setIf(extMap["zkne"], RV_ZKNE)
	c.featureSet.setIf(extMap["zknh"], RV_ZKNH)
	c.featureSet.setIf(extMap["zksed"], RV_ZKSED)
	c.featureSet.setIf(extMap["zksh"], RV_ZKSH)
	c.featureSet.setIf(extMap["zkt"], RV_ZKT)

	// Vector cryptography
	c.featureSet.setIf(extMap["zvbb"], RV_ZVBB)
	c.featureSet.setIf(extMap["zvbc"], RV_ZVBC)
	c.featureSet.setIf(extMap["zvkb"], RV_ZVKB)
	c.featureSet.setIf(extMap["zvkg"], RV_ZVKG)
	c.featureSet.setIf(extMap["zvkned"], RV_ZVKNED)
	c.featureSet.setIf(extMap["zvknha"], RV_ZVKNHA)
	c.featureSet.setIf(extMap["zvknhb"], RV_ZVKNHB)
	c.featureSet.setIf(extMap["zvksed"], RV_ZVKSED)
	c.featureSet.setIf(extMap["zvksh"], RV_ZVKSH)
	c.featureSet.setIf(extMap["zvkt"], RV_ZVKT)

	// Crypto suites (combined from individual features or bundle tokens)
	c.featureSet.setIf(extMap["zkn"] || c.featureSet.hasSetP(rvZKNFeatures), RV_ZKN)
	c.featureSet.setIf(extMap["zks"] || c.featureSet.hasSetP(rvZKSFeatures), RV_ZKS)
	c.featureSet.setIf(extMap["zvkng"] || c.featureSet.hasSetP(rvZVKNFeatures), RV_ZVKNG)
	c.featureSet.setIf(extMap["zvksg"] || c.featureSet.hasSetP(rvZVKSFeatures), RV_ZVKSG)
}

var riscvVendorMap = map[uint64]Vendor{
	0x489: SiFive,
	0x5b7: StarFive,
	0x5b1: THead,
	0x31e: Andes,
	0x710: SpacemiT,
}

func riscvVendorID(mvendorid uint64) Vendor {
	if v, ok := riscvVendorMap[mvendorid]; ok {
		return v
	}
	return VendorUnknown
}
