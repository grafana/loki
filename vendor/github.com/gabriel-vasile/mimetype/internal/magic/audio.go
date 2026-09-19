package magic

import (
	"bytes"
	"encoding/binary"

	"github.com/gabriel-vasile/mimetype/internal/mp3"
)

// Flac matches a Free Lossless Audio Codec file.
func Flac(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("\x66\x4C\x61\x43\x00\x00\x00\x22"))
}

// Midi matches a Musical Instrument Digital Interface file.
func Midi(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("\x4D\x54\x68\x64"))
}

// Ape matches a Monkey's Audio file.
func Ape(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("\x4D\x41\x43\x20\x96\x0F\x00\x00\x34\x00\x00\x00\x18\x00\x00\x00\x90\xE3"))
}

// MusePack matches a Musepack file.
func MusePack(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("MPCK"))
}

// Au matches a Sun Microsystems au file.
func Au(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("\x2E\x73\x6E\x64"))
}

// Amr matches an Adaptive Multi-Rate file.
func Amr(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("\x23\x21\x41\x4D\x52"))
}

// Voc matches a Creative Voice file.
func Voc(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("Creative Voice File"))
}

// M3U matches a Playlist file.
func M3U(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("#EXTM3U\n")) ||
		bytes.HasPrefix(raw, []byte("#EXTM3U\r\n"))
}

// AAC matches an Advanced Audio Coding file.
func AAC(raw []byte, _ uint32) bool {
	return len(raw) > 1 && ((raw[0] == 0xFF && raw[1] == 0xF1) || (raw[0] == 0xFF && raw[1] == 0xF9))
}

// MP3 matches a .mp3 file.
func MP3(raw []byte, limit uint32) bool {
	if len(raw) < 3 {
		return false
	}

	// Any ID3v2 is reported as MP3. Not entirely correct, but the mimesniff
	// standard says so. https://mimesniff.spec.whatwg.org/#matching-an-audio-or-video-type-pattern
	// Despite the standard only checking for "ID3", we do more validations to
	// avoid false positives.
	if id3v2(raw) {
		return true
	}

	// If no ID3v2 tag found, then we will look for MP3 frames, but:
	// a. Layer III files are a lot more prevalent than Layer I and II.
	// b. Layer I frame header has looser constraints than the others: many files
	// with regularly repeating 0xFFFF bytes can be misidentified as MP3.
	// c. MP3 files are composed of individual frames and those frames can have
	// leading garbage bytes: if we want to find all valid MP3s, we have to do a
	// linear search. #775, #310
	// d. There are file formats that contain MP3s inside: .mo3 and .swa
	//
	// Given a, b, c and d, this code:
	// - initially tries to match by first two bytes in header
	// - checks for .mo3 and .swa and disqualifies them
	// - does linear search for Layer III
	switch binary.BigEndian.Uint16(raw[:2]) & 0xFFFE {
	case 0xFFFA, 0xFFF2, 0xFFE2, // layer III: v1, v2, v2.5
		0xFFFC, 0xFFF4, // layer II: v1, v2
		0xFFF5: // layer I: v2
		return true
	}
	// http://lclevy.free.fr/mo3/
	if bytes.HasPrefix(raw, []byte("MO3")) {
		return false
	}

	// From PRONOM:
	// Macromedia licensed the MP3 technology in 1995 to use in their Shockwave
	// product. .swa or Shockwave Audio was originally added as a free plugin
	// (Xtras) to SoundEdit 16 to export AIFF files to .swa.
	// There is no media type assigned for .swa.
	if bytes.HasPrefix(raw, []byte{0x00, 0x00, 0x01, 0x40, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00}) {
		return false
	}

	_, size := mp3.ExtractFrame(raw)
	return size > 0
}

// Based on https://id3.org/Developer%20Information.
func id3v2(raw []byte) bool {
	if len(raw) < 10 || !bytes.HasPrefix(raw, []byte("ID3")) {
		return false
	}
	if raw[3] < 2 || raw[3] > 4 { // Version: ID3v2.2 - ID3v2.4.
		return false
	}
	if raw[4] != 0 { // Revision is 0 for all versions.
		return false
	}

	// v2.2 uses 2 bits, v2.3 uses 3 bits and v2.4 uses 4.
	// For all versions least significant 4 bits should be 0
	if raw[5]&0b1111 != 0 {
		return false
	}

	// Size bytes are synchsafe: most significant bit always 0.
	if raw[6]&0x80 != 0 || raw[7]&0x80 != 0 || raw[8]&0x80 != 0 || raw[9]&0x80 != 0 {
		return false
	}

	size := uint32(raw[6])<<21 | uint32(raw[7])<<14 | uint32(raw[8])<<7 | uint32(raw[9])
	// Disallow too big frames, let's say 10MB.
	return size > 0 && size < 10*1024*1024
}

// Wav matches a Waveform Audio File Format file.
func Wav(raw []byte, limit uint32) bool {
	return len(raw) > 12 &&
		bytes.Equal(raw[:4], []byte("RIFF")) &&
		bytes.Equal(raw[8:12], []byte{0x57, 0x41, 0x56, 0x45})
}

// Aiff matches Audio Interchange File Format file.
func Aiff(raw []byte, limit uint32) bool {
	return len(raw) > 12 &&
		bytes.Equal(raw[:4], []byte{0x46, 0x4F, 0x52, 0x4D}) &&
		bytes.Equal(raw[8:12], []byte{0x41, 0x49, 0x46, 0x46})
}

// Qcp matches a Qualcomm Pure Voice file.
func Qcp(raw []byte, limit uint32) bool {
	return len(raw) > 12 &&
		bytes.Equal(raw[:4], []byte("RIFF")) &&
		bytes.Equal(raw[8:12], []byte("QLCM"))
}
