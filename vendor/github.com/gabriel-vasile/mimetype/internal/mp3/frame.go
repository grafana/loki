package mp3

import "bytes"

// minTruncatedSyncMatches is the minimum number of confirmed successive
// header matches required to accept a candidate frame when the buffer ends
// before maxFrameSyncMatches confirmations can be performed.
const minTruncatedSyncMatches = 2

func ExtractFrame(b []byte) (start, size int) {
	limit := min(len(b), 2048+headerSize)
	for i := 0; i < limit-headerSize; i++ {
		j := bytes.IndexByte(b[i:limit-headerSize], 0xFF)
		if j < 0 {
			break
		}
		i += j
		hdr := header{b[i], b[i+1], b[i+2], b[i+3]}
		if !hdr.valid() {
			continue
		}
		frameBytes := hdr.frameBytes()
		frameAndPad := frameBytes + hdr.padding()

		validHere := frameBytes > 0 && i+frameAndPad <= len(b) && matchFrame(b[i:])
		// When the buffer is exactly one frame, matchFrame cannot look ahead for
		// a subsequent header to confirm the stream. Trust the validated header.
		exact := i == 0 && frameAndPad == len(b)
		if validHere || exact {
			return i, frameAndPad
		}
	}
	return 0, 0
}

// matchFrame confirms a candidate header by stepping forward and checking that
// subsequent headers are consistent.
func matchFrame(buf []byte) bool {
	// maxFrameSyncMatches limits how many valid frames we look at.
	const maxFrameSyncMatches = 10
	hdr := header{buf[0], buf[1], buf[2], buf[3]}
	i := hdr.frameBytes() + hdr.padding()
	for nmatch := 0; nmatch < maxFrameSyncMatches; nmatch++ {
		if i+headerSize > len(buf) {
			return nmatch >= minTruncatedSyncMatches
		}
		cmp := header{buf[i], buf[i+1], buf[i+2], buf[i+3]}
		if !hdr.compatibleWith(cmp) {
			return false
		}
		i += cmp.frameBytes() + cmp.padding()
	}
	return true
}

const headerSize = 4

type header [headerSize]byte

func (h header) isFreeFormat() bool  { return h[2]&0xF0 == 0 }
func (h header) isMPEG1() bool       { return h[1]&0x8 != 0 }
func (h header) isMPEG25() bool      { return h[1]&0x10 == 0 }
func (h header) rawLayer() byte      { return h[1] >> 1 & 3 }
func (h header) rawBitrate() byte    { return h[2] >> 4 }
func (h header) rawSampleRate() byte { return h[2] >> 2 & 3 }
func (h header) rawEmphasis() byte   { return h[3] & 0b11 }
func (h header) isFrame576() bool    { return h[1]&14 == 2 }
func (h header) padding() int {
	if h[2]&0x2 != 0 {
		return 1
	}
	return 0
}

// valid reports whether the four bytes form a syntactically valid MP3 header.
func (h header) valid() bool {
	return h[0] == 0xff &&
		((h[1]&0xF0) == 0xf0 || (h[1]&0xFE) == 0xe2) &&
		h.rawLayer() == 1 && // Layer III
		h.rawBitrate() != 15 && // Not allowed by spec.
		h.rawSampleRate() != 3 &&
		h.rawEmphasis() != 2 &&
		// The code for extracting frame size for free-format is tedious and
		// free-format MP3s are extinct.
		!h.isFreeFormat()
}

// compatibleWith reports whether two headers describe frames belonging to the
// same MP3 stream — same MPEG version, layer, sample-rate index.
func (h header) compatibleWith(o header) bool {
	return o.valid() &&
		(h[1]^o[1])&0xFE == 0 &&
		(h[2]^o[2])&0x0C == 0
}

// bitrateKbps returns the bitrate of the frame in kilobits per second.
func (h header) bitrateKbps() int {
	// halfrate[mpeg1?][bitrate_idx] holds bitrate/2 in kbps.
	halfrate := [2][15]uint8{
		{0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 72, 80},
		{0, 16, 20, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160},
	}
	mpeg1 := 0
	if h.isMPEG1() {
		mpeg1 = 1
	}
	return 2 * int(halfrate[mpeg1][h.rawBitrate()])
}

// sampleRateHz returns the sampling rate of the frame in Hz.
func (h header) sampleRateHz() int {
	base := [3]int{44100, 48000, 32000}[h.rawSampleRate()]
	if !h.isMPEG1() {
		base >>= 1
	}
	if h.isMPEG25() {
		base >>= 1
	}
	return base
}

// frameSamples returns the number of audio samples per channel encoded in
// the frame.
func (h header) frameSamples() int {
	if h.isFrame576() {
		return 576
	}
	return 1152
}

// frameBytes returns the size of the frame body (header + side info + audio
// data, excluding padding) in bytes.
func (h header) frameBytes() int {
	br := h.bitrateKbps()
	sr := h.sampleRateHz()
	if br == 0 || sr == 0 {
		return 0
	}
	return h.frameSamples() * br * 125 / sr
}
