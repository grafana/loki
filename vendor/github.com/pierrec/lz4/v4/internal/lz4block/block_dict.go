package lz4block

import "encoding/binary"

// dictData indexes the dictionary for CompressBlockWithDict.
type dictData struct {
	table [htSize]uint16
	inUse [htSize / 32]uint32
}

// initDict sets the dictionary, keeping only its last winSize bytes, and
// indexes it unless it is the dictionary already indexed.
func (c *Compressor) initDict(dict []byte) {
	if len(dict) > winSize {
		dict = dict[len(dict)-winSize:]
	}
	if len(dict) == 0 {
		c.dict = nil
		return
	}
	if len(c.dict) == len(dict) && &c.dict[0] == &dict[0] {
		return
	}
	c.dict = dict
	if c.dictData == nil {
		c.dictData = new(dictData)
	} else {
		c.dictData.inUse = [htSize / 32]uint32{}
	}
	// Index every third position, like LZ4_loadDict.
	for j := 0; j+8 <= len(dict); j += 3 {
		h := blockHash(binary.LittleEndian.Uint64(dict[j:])) & (htSize - 1)
		c.dictData.table[h] = uint16(j)
		c.dictData.inUse[h/32] |= 1 << (h % 32)
	}
}

func CompressBlockWithDict(src, dst, dict []byte) (int, error) {
	c := compressorPool.Get().(*Compressor)
	n, err := c.CompressBlockWithDict(src, dst, dict)
	// Don't keep the caller's dictionary alive. dictData's allocation is
	// reused, but the next call indexes its dictionary again.
	c.dict = nil
	compressorPool.Put(c)
	return n, err
}

// CompressBlockWithDict is like CompressBlock, but matches may also refer to
// the last 64 KiB of dict, as for UncompressBlock with the same dict.
// Calling it again with the same dict slice reuses the dictionary's index.
//
// It uses its own loop so that CompressBlock, the common case, does not pay
// for the dictionary.
func (c *Compressor) CompressBlockWithDict(src, dst, dict []byte) (int, error) {
	c.initDict(dict)
	if len(c.dict) == 0 {
		return c.CompressBlock(src, dst)
	}
	return c.compressBlockDict(src, dst)
}

// dictPos returns the position in the dictionary of a match for the 4 bytes
// x at si, with h the hash of the 8 bytes there.
func (c *Compressor) dictPos(h uint32, si int, x uint32) (int, bool) {
	h &= htSize - 1
	if c.dictData.inUse[h/32]&(1<<(h%32)) == 0 {
		return 0, false
	}
	p := int(c.dictData.table[h])
	if off := si + len(c.dict) - p; off >= winSize || binary.LittleEndian.Uint32(c.dict[p:]) != x {
		return 0, false
	}
	return p, true
}

// compressBlockDict compresses src with matches in src or in c.dict. It
// follows CompressBlock, with one candidate per position and no unsafe loads.
func (c *Compressor) compressBlockDict(src, dst []byte) (_ int, err error) {
	// As in compressBlockHC, a short dst is caught by bounds checks and
	// recovered here. Cap it so that copies cannot reslice past its length.
	defer recoverBlock(&err)
	dst = dst[:len(dst):len(dst)]
	c.reset()
	isNotCompressible := len(dst) < CompressBlockBound(len(src))
	const adaptSkipLog = 7
	dict := c.dict

	var si, di, anchor int
	sn := len(src) - mfLimit
	for si < sn {
		match := binary.LittleEndian.Uint64(src[si:])
		h := blockHash(match)
		ref := c.get(h, si)
		c.put(h, si)

		lLen := si - anchor
		var offset, n int
		if off := si - ref; ref >= 0 && off < winSize && uint32(match) == binary.LittleEndian.Uint32(src[ref:]) {
			offset = off
			for lLen > 0 && ref > 0 && src[si-1] == src[ref-1] {
				si--
				ref--
				lLen--
			}
			n = minMatch + matchLength(src, ref+minMatch, si+minMatch, sn)
		} else if p, ok := c.dictPos(h, si, uint32(match)); ok {
			offset = si + len(dict) - p
			for lLen > 0 && p > 0 && src[si-1] == dict[p-1] {
				si--
				p--
				lLen--
			}
			n = minMatch
			for p+n < len(dict) && si+n < sn && src[si+n] == dict[p+n] {
				n++
			}
			if p+n == len(dict) {
				// The match continues into src, offset bytes back: at src[0].
				n += matchLength(src, 0, si+n, sn)
			}
		} else {
			si += 1 + lLen>>adaptSkipLog
			continue
		}

		di = writeSequence(dst, di, src[anchor:si], offset, n-minMatch)
		si += n
		anchor = si
		if si < sn {
			c.put(blockHash(binary.LittleEndian.Uint64(src[si-2:])), si-2)
		}
	}

	if isNotCompressible && anchor == 0 {
		// Incompressible.
		return 0, nil
	}
	// As in CompressBlock, give up before the last literals if the output
	// is no smaller than the input consumed so far.
	lLen := len(src) - anchor
	dst[di] = byte(min(lLen, 0xF) << 4)
	di = writeLenExt(dst, di+1, lLen)
	if isNotCompressible && di >= anchor {
		// Incompressible.
		return 0, nil
	}
	return di + copy(dst[di:di+lLen], src[anchor:]), nil
}

// writeSequence writes the literals lit and a match of length mLen+minMatch
// at offset to dst at di, and returns the new position in dst.
func writeSequence(dst []byte, di int, lit []byte, offset, mLen int) int {
	lLen := len(lit)
	dst[di] = byte(min(lLen, 0xF)<<4 | min(mLen, 0xF))
	di = writeLenExt(dst, di+1, lLen)
	di += copy(dst[di:di+lLen], lit)
	dst[di], dst[di+1] = byte(offset), byte(offset>>8)
	return writeLenExt(dst, di+2, mLen)
}

// writeLenExt writes the bytes extending a literal or match length n that
// does not fit in its token nibble, and returns the new position in dst.
func writeLenExt(dst []byte, di, n int) int {
	if n < 0xF {
		return di
	}
	for n -= 0xF; n >= 0xFF; n -= 0xFF {
		dst[di] = 0xFF
		di++
	}
	dst[di] = byte(n)
	return di + 1
}
