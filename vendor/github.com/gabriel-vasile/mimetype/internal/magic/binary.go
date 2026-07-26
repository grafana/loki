package magic

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"slices"
)

// Lnk matches Microsoft lnk binary format.
func Lnk(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte{0x4C, 0x00, 0x00, 0x00, 0x01, 0x14, 0x02, 0x00})
}

// Wasm matches a web assembly File Format file.
func Wasm(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte{0x00, 0x61, 0x73, 0x6D})
}

// Exe matches a Windows/DOS executable file.
func Exe(raw []byte, _ uint32) bool {
	return len(raw) > 1 && raw[0] == 0x4D && raw[1] == 0x5A
}

// Elf matches an Executable and Linkable Format file.
func Elf(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte{0x7F, 0x45, 0x4C, 0x46})
}

// Nes matches a Nintendo Entertainment system ROM file.
func Nes(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte{0x4E, 0x45, 0x53, 0x1A})
}

// SWF matches an Adobe Flash swf file.
func SWF(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("CWS")) ||
		bytes.HasPrefix(raw, []byte("FWS")) ||
		bytes.HasPrefix(raw, []byte("ZWS"))
}

// Torrent has bencoded text in the beginning.
func Torrent(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("d8:announce"))
}

// PAR1 matches a parquet file.
func Par1(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte{0x50, 0x41, 0x52, 0x31})
}

// CBOR matches a Concise Binary Object Representation https://cbor.io/
func CBOR(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte{0xD9, 0xD9, 0xF7})
}

// Java bytecode and Mach-O binaries share the same magic number.
// More info here https://github.com/threatstack/libmagic/blob/master/magic/Magdir/cafebabe
func classOrMachOFat(in []byte) bool {
	// There should be at least 8 bytes for both of them because the only way to
	// quickly distinguish them is by comparing byte at position 7
	if len(in) < 8 {
		return false
	}

	return binary.BigEndian.Uint32(in) == macho.MagicFat
}

// Class matches a java class file.
func Class(raw []byte, limit uint32) bool {
	return classOrMachOFat(raw) && raw[7] > 30
}

// MachO matches Mach-O binaries format.
func MachO(raw []byte, limit uint32) bool {
	if classOrMachOFat(raw) && raw[7] < 0x14 {
		return true
	}

	if len(raw) < 4 {
		return false
	}

	be := binary.BigEndian.Uint32(raw)
	le := binary.LittleEndian.Uint32(raw)

	return be == macho.Magic32 ||
		le == macho.Magic32 ||
		be == macho.Magic64 ||
		le == macho.Magic64
}

// Dbf matches a dBase file.
// https://www.dbase.com/Knowledgebase/INT/db7_file_fmt.htm
func Dbf(raw []byte, limit uint32) bool {
	if len(raw) < 68 {
		return false
	}

	// 3rd and 4th bytes contain the last update month and day of month.
	if raw[2] == 0 || raw[2] > 12 || raw[3] == 0 || raw[3] > 31 {
		return false
	}

	// 12, 13, 30, 31 are reserved bytes and always filled with 0x00.
	if raw[12] != 0x00 || raw[13] != 0x00 || raw[30] != 0x00 || raw[31] != 0x00 {
		return false
	}
	// Production MDX flag;
	// 0x01 if a production .MDX file exists for this table;
	// 0x00 if no .MDX file exists.
	if raw[28] > 0x01 {
		return false
	}

	// dbf type is dictated by the first byte.
	dbfTypes := []byte{
		0x02, 0x03, 0x04, 0x05, 0x30, 0x31, 0x32, 0x42, 0x62, 0x7B, 0x82,
		0x83, 0x87, 0x8A, 0x8B, 0x8E, 0xB3, 0xCB, 0xE5, 0xF5, 0xF4, 0xFB,
	}
	return slices.Contains(dbfTypes, raw[0])
}

// ElfObj matches an object file.
func ElfObj(raw []byte, limit uint32) bool {
	return len(raw) > 17 && ((raw[16] == 0x01 && raw[17] == 0x00) ||
		(raw[16] == 0x00 && raw[17] == 0x01))
}

// ElfExe matches an executable file.
func ElfExe(raw []byte, limit uint32) bool {
	return len(raw) > 17 && ((raw[16] == 0x02 && raw[17] == 0x00) ||
		(raw[16] == 0x00 && raw[17] == 0x02))
}

// ElfLib matches a shared library file.
func ElfLib(raw []byte, limit uint32) bool {
	return len(raw) > 17 && ((raw[16] == 0x03 && raw[17] == 0x00) ||
		(raw[16] == 0x00 && raw[17] == 0x03))
}

// ElfDump matches a core dump file.
func ElfDump(raw []byte, limit uint32) bool {
	return len(raw) > 17 && ((raw[16] == 0x04 && raw[17] == 0x00) ||
		(raw[16] == 0x00 && raw[17] == 0x04))
}

// Dcm matches a DICOM medical format file.
func Dcm(raw []byte, limit uint32) bool {
	return len(raw) > 131 &&
		bytes.Equal(raw[128:132], []byte{0x44, 0x49, 0x43, 0x4D})
}

// Marc matches a MARC21 (MAchine-Readable Cataloging) file.
func Marc(raw []byte, limit uint32) bool {
	// File is at least 24 bytes ("leader" field size).
	if len(raw) < 24 {
		return false
	}

	// Fixed bytes at offset 20.
	if !bytes.Equal(raw[20:24], []byte("4500")) {
		return false
	}

	// First 5 bytes are ASCII digits.
	for i := 0; i < 5; i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}

	// Field terminator is present in first 2048 bytes.
	return bytes.Contains(raw[:min(2048, len(raw))], []byte{0x1E})
}

// GLB matches a glTF model format file.
// GLB is the binary file format representation of 3D models saved in
// the GL transmission Format (glTF).
// GLB uses little endian and its header structure is as follows:
//
//	<-- 12-byte header                             -->
//	| magic            | version          | length   |
//	| (uint32)         | (uint32)         | (uint32) |
//	| \x67\x6C\x54\x46 | \x01\x00\x00\x00 | ...      |
//	| g   l   T   F    | 1                | ...      |
//
// Visit [glTF specification] and [IANA glTF entry] for more details.
//
// [glTF specification]: https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html
// [IANA glTF entry]: https://www.iana.org/assignments/media-types/model/gltf-binary
func GLB(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("\x67\x6C\x54\x46\x02\x00\x00\x00")) ||
		bytes.HasPrefix(raw, []byte("\x67\x6C\x54\x46\x01\x00\x00\x00"))
}

// TzIf matches a Time Zone Information Format (TZif) file.
// See more: https://tools.ietf.org/id/draft-murchison-tzdist-tzif-00.html#rfc.section.3
// Its header structure is shown below:
//
//	+---------------+---+
//	|  magic    (4) | <-+-- version (1)
//	+---------------+---+---------------------------------------+
//	|           [unused - reserved for future use] (15)         |
//	+---------------+---------------+---------------+-----------+
//	|  isutccnt (4) |  isstdcnt (4) |  leapcnt  (4) |
//	+---------------+---------------+---------------+
//	|  timecnt  (4) |  typecnt  (4) |  charcnt  (4) |
func TzIf(raw []byte, limit uint32) bool {
	// File is at least 44 bytes (header size).
	if len(raw) < 44 {
		return false
	}

	if !bytes.HasPrefix(raw, []byte("TZif")) {
		return false
	}

	// Field "typecnt" MUST not be zero.
	if binary.BigEndian.Uint32(raw[36:40]) == 0 {
		return false
	}

	// Version has to be NUL (0x00), '2' (0x32) or '3' (0x33).
	return raw[4] == 0x00 || raw[4] == 0x32 || raw[4] == 0x33
}

// Pyc matches a Python compiled file.
// The signatures are sourced from libmagic v5.47
func Pyc(raw []byte, limit uint32) bool {
	if len(raw) < 8 {
		return false
	}

	// python 1.0 through 3.7 signatures, magic/Magdir/python:13:190
	pycMagic := []uint32{
		0x02099900, 0x03099900, 0x892e0d0a, 0x04170d0a, 0x994e0d0a, 0xfcc40d0a,
		0xfdc40d0a, 0x87c60d0a, 0x88c60d0a, 0x2aeb0d0a, 0x2beb0d0a, 0x2ded0d0a,
		0x2eed0d0a, 0x3bf20d0a, 0x3cf20d0a, 0x45f20d0a, 0x59f20d0a, 0x63f20d0a,
		0x6df20d0a, 0x6ef20d0a, 0x77f20d0a, 0x81f20d0a, 0x8bf20d0a, 0x8cf20d0a,
		0x95f20d0a, 0x9ff20d0a, 0xa9f20d0a, 0xb3f20d0a, 0xb4f20d0a, 0xc7f20d0a,
		0xd1f20d0a, 0xd2f20d0a, 0xdbf20d0a, 0xe5f20d0a, 0xeff20d0a, 0xf9f20d0a,
		0x03f30d0a, 0x04f30d0a, 0x0af30d0a, 0xb80b0d0a, 0xc20b0d0a, 0xcc0b0d0a,
		0xd60b0d0a, 0xe00b0d0a, 0xea0b0d0a, 0xf40b0d0a, 0xf50b0d0a, 0xff0b0d0a,
		0x090c0d0a, 0x130c0d0a, 0x1d0c0d0a, 0x1f0c0d0a, 0x270c0d0a, 0x3b0c0d0a,
		0x450c0d0a, 0x4f0c0d0a, 0x580c0d0a, 0x620c0d0a, 0x6c0c0d0a, 0x760c0d0a,
		0x800c0d0a, 0x8a0c0d0a, 0x940c0d0a, 0x9e0c0d0a, 0xb20c0d0a, 0xbc0c0d0a,
		0xc60c0d0a, 0xd00c0d0a, 0xda0c0d0a, 0xe40c0d0a, 0xee0c0d0a, 0xf80c0d0a,
		0x020d0d0a, 0x0c0d0d0a, 0x160d0d0a, 0x170d0d0a, 0x200d0d0a, 0x210d0d0a,
		0x2a0d0d0a, 0x2b0d0d0a, 0x2c0d0d0a, 0x2d0d0d0a, 0x2f0d0d0a, 0x300d0d0a,
		0x310d0d0a, 0x320d0d0a, 0x330d0d0a, 0x3e0d0d0a, 0x3f0d0d0a,
	}

	n := binary.BigEndian.Uint32(raw)

	if slices.Contains(pycMagic, n) {
		return true
	}

	if raw[2] == 0x0d && raw[3] == 0x0a {
		// Only two bits of flag field are currently used.
		if l := binary.LittleEndian.Uint32(raw[4:]); l > 3 {
			return false
		}
		if raw[1] == 0x0d || raw[1] == 0x0e {
			return true
		}
		// PyPy magic numbers, magic/Magdir/python:233
		n := binary.LittleEndian.Uint16(raw)
		return n == 240 || n == 256 || n == 336 || n == 384 || n == 416
	}

	return false
}

// Pcap identifies "libpcap" capture files.
// https://www.tcpdump.org/manpages/pcap-savefile.5.html
func Pcap(raw []byte, _ uint32) bool {
	if len(raw) < 4 {
		return false
	}
	be := binary.BigEndian.Uint32(raw)
	le := binary.LittleEndian.Uint32(raw)
	return be == 0xa1b2c3d4 || be == 0xa1b23c4d ||
		le == 0xa1b2c3d4 || le == 0xa1b23c4d
}
