// Package cdf implements parsing of CDF (OLE2) files. It is greatly inspired
// by src/readcdf.c from libmagic. One difference is this implementation is
// permissive of truncated inputs. See readLimit in mimetype.go for the
// reason why truncated inputs need to be handled.
// http://sc.openoffice.org/compdocfileformat.pdf
package cdf

import (
	"bytes"
	"encoding/binary"

	"github.com/gabriel-vasile/mimetype/internal/scan"
)

type CDFType int8

const (
	CDFTypeGeneric CDFType = iota
	CDFTypeInstaller
	CDFTypeDoc
	CDFTypePpt
	CDFTypeXls
	CDFTypeMsg
)

// Detect parses raw as a CDF (OLE2) compound file and returns the document type
// it contains. It returns CDFTypeGeneric for input that is not a CDF file or
// whose type cannot be narrowed down.
func Detect(raw []byte) CDFType {
	if len(raw) < 512 {
		return CDFTypeGeneric
	}
	var c cdf
	if !parse(raw, &c) {
		return CDFTypeGeneric
	}
	return c.detect()
}

// cdf holds everything we need from a CDF file to do detection.
type cdf struct {
	data            []byte
	secSize         int
	shortSecSize    int
	minStdStream    uint32
	satSecs         int32s // list of SAT sector ids; usually a sub-slice of raw input
	satEntries      int    // number of valid SAT entries reachable through satSecs
	firstSSAT       int32
	dirRaw          []byte // directory stream bytes (entries are decoded on demand)
	sst             []byte // short-stream pool (root storage's stream)
	sstBuilt        bool   // whether sst was already loaded (it is loaded lazily)
	rootStreamFirst int32  // first sector of the root storage short-stream pool
	rootStreamSize  uint32 // size of the root storage short-stream pool
	rootStorageUUID []byte
}

// parse reads the entire on-disk structure required for type detection. It
// returns true on success and false if the header does not look like a CDF file.
// Truncated or partially malformed bodies are tolerated: sector reads degrade
// to whatever could be collected so detection can still succeed from partial data.
func parse(raw []byte, c *cdf) bool {
	if len(raw) < 512 || binary.LittleEndian.Uint64(raw) != cdfMagic {
		return false
	}
	secP2 := binary.LittleEndian.Uint16(raw[30:32])
	shortP2 := binary.LittleEndian.Uint16(raw[32:34])
	if secP2 > 20 || shortP2 > 20 {
		return false
	}
	c.data = raw
	c.secSize = 1 << secP2
	c.shortSecSize = 1 << shortP2
	c.minStdStream = binary.LittleEndian.Uint32(raw[56:60])
	if c.secSize < dirEntrySize {
		return false
	}
	firstDirSec := readSecID(raw[48:52])
	c.firstSSAT = readSecID(raw[60:64])
	firstMSAT := readSecID(raw[68:72])
	nMSAT := binary.LittleEndian.Uint32(raw[72:76])
	masterSAT := int32s{b: raw[76 : 76+4*masterSATSize]}

	c.buildSAT(masterSAT, firstMSAT, nMSAT)
	c.dirRaw = c.readLong(firstDirSec, 0)

	c.rootStreamFirst = -1
	var d dirEntry
	for i, n := 0, c.dirLen(); i < n; i++ {
		c.dirAt(i, &d)
		if d.typ != dirTypeRootStorage || d.streamFirst < 0 {
			continue
		}
		c.rootStorageUUID = d.storageUUID[:]
		// Record where the short-stream pool lives; it is loaded lazily by
		// shortStream the first time a short stream is actually read.
		c.rootStreamFirst = d.streamFirst
		c.rootStreamSize = d.size
		break
	}
	return true
}

func (c *cdf) detect() CDFType {
	for _, name := range []string{"\x05SummaryInformation", "\x05DocumentSummaryInformation"} {
		if t, ok := c.detectFromSummary(name); ok {
			return t
		}
	}
	var d dirEntry
	for i, n := 0, c.dirLen(); i < n; i++ {
		c.dirAt(i, &d)
		if t, ok := lookupSection(d.nameBytes(), d.typ); ok {
			return t
		}
	}
	return CDFTypeGeneric
}

// detectFromSummary inspects a (Doc)SummaryInformation stream and tries to
// derive a CDFType from the root-storage CLSID, the property NameOfApplication,
// and finally the names of sibling user streams.
func (c *cdf) detectFromSummary(streamName string) (CDFType, bool) {
	if c.rootStorageUUID != nil && bytes.Equal(c.rootStorageUUID, msiCLSID) {
		return CDFTypeInstaller, true
	}
	raw, ok := c.userStream(streamName)
	if !ok {
		return CDFTypeGeneric, false
	}
	if app := summaryAppName(raw); len(app) > 0 {
		if t, ok := lookupSubstring(app, app2type); ok {
			return t, true
		}
	}
	for i, n := 0, c.dirLen(); i < n; i++ {
		var d dirEntry
		c.dirAt(i, &d)
		if d.nameLen == 0 {
			continue
		}
		if t, ok := lookupSubstring(d.nameBytes(), name2type); ok {
			return t, true
		}
	}
	return CDFTypeGeneric, true
}

const (
	cdfMagic uint64 = 0xE11AB1A1E011CFD0

	dirTypeUserStorage = 1
	dirTypeUserStream  = 2
	dirTypeRootStorage = 5

	dirEntrySize  = 128
	masterSATSize = 109 // first 109 SAT secids live in the file header
)

// dirEntry is a single CDF directory record. The UTF-16LE name is pre-decoded
// into an inline ASCII buffer at parse time, avoiding a per-entry heap
// allocation while keeping comparisons trivial. CDF names are at most 32
// UTF-16 code units, so 32 bytes always suffice.
type dirEntry struct {
	name        [32]byte
	nameLen     uint8
	typ         uint8
	streamFirst int32
	size        uint32
	storageUUID [16]byte
}

// nameBytes returns the decoded ASCII name without copying.
func (d *dirEntry) nameBytes() []byte { return d.name[:d.nameLen] }

func (c *cdf) ssatAt(i int32) int32 {
	for sid := c.firstSSAT; sid >= 0; {
		if int(sid) >= c.satLen() {
			break // SAT is truncated; stop collecting
		}
		buf, ok := c.sector(sid)
		if !ok {
			break
		}
		lbuf := int32(len(buf) / 4) //nolint:gosec // anything divided by 4 fits int32
		if i < lbuf {
			return int32(binary.LittleEndian.Uint32(buf[4*i:])) //nolint:gosec // intentional two's-complement reinterpretation of a sector id
		}
		i -= lbuf
		sid = c.satAt(sid)
	}
	return -1
}

// shortStream returns the root storage short-stream pool, loading it on first
// use. Detection often finishes (e.g. via the root CLSID or a long-stream
// summary) without ever reading a short stream, so building this eagerly would
// be wasted work.
func (c *cdf) shortStream() []byte {
	if !c.sstBuilt {
		c.sstBuilt = true
		if c.rootStreamFirst >= 0 {
			c.sst = c.readLong(c.rootStreamFirst, c.rootStreamSize)
		}
	}
	return c.sst
}

// int32s works like a slice of LE int32 and is backed by a slice of bytes.
// int32s could very well be type int32s []byte, but that would mean
// len function can be called on it. We don't want that, we always want to use
// the len method.
type int32s struct {
	b []byte
}

func (b int32s) at(i int) int32 {
	//nolint:gosec // intentional two's-complement reinterpretation of a sector id
	return int32(binary.LittleEndian.Uint32(b.b[4*i:]))
}
func (b int32s) len() int {
	return len(b.b) / 4
}

// readSecID reinterprets four little-endian bytes as a signed sector id.
// Every 32-bit pattern is a valid id (values >= 0 are sector numbers,
// negatives are CDF sentinels such as -2 end-of-chain), so the conversion is
// an intentional two's-complement reinterpretation rather than an overflow.
func readSecID(b []byte) int32 {
	return int32(binary.LittleEndian.Uint32(b)) //nolint:gosec // intentional two's-complement reinterpretation
}

// satLen is the number of sector ids reachable through the SAT.
func (c *cdf) satLen() int { return c.satEntries }

// satAt returns the i-th sector id from the SAT. Callers must ensure
// i < satLen(). The SAT is not materialized; the entry is fetched directly
// from the input by translating i into (SAT sector index, entry offset).
func (c *cdf) satAt(i int32) int32 {
	perSec := c.secSize / 4
	secIdx := int(i) / perSec
	entryIdx := int(i) % perSec
	secID := c.satSecs.at(secIdx)
	off := c.secSize*(1+int(secID)) + 4*entryIdx
	return readSecID(c.data[off:])
}

// sector returns the bytes of long sector secid. If the file is truncated
// inside the requested sector the result is the available bytes (no padding).
// If the sector starts past EOF or secid is negative, then ok is false.
func (c *cdf) sector(secid int32) (_ []byte, ok bool) {
	if secid < 0 {
		return nil, false
	}
	off := int64(c.secSize) * (1 + int64(secid))
	if off >= int64(len(c.data)) {
		return nil, false
	}
	// The returned sector might be truncated,
	// but we still return it as best effort.
	end := min(off+int64(c.secSize), int64(len(c.data)))
	// If not even one int32 fits, then fail.
	if end-off < 4 {
		return nil, false
	}
	return c.data[off:end], true
}

func (c *cdf) sectorIDs(secid int32) (int32s, bool) {
	buf, ok := c.sector(secid)
	if !ok {
		return int32s{}, ok
	}
	return int32s{b: buf}, true
}

// buildSAT records the list of SAT sector ids from the master-SAT (header)
// plus any extension blocks chained via firstMSAT. The SAT itself is not
// materialized: satAt computes the requested entry directly from c.data via
// satSecs. In the common case (no extension chain) satSecs is a zero-copy
// sub-slice of the input header.
func (c *cdf) buildSAT(masterSAT int32s, firstMSAT int32, nMSAT uint32) {
	// Fast path: no extension chain. masterSAT is already a sub-slice of raw
	// input; reuse it directly.
	if firstMSAT < 0 || nMSAT == 0 {
		c.satSecs = masterSAT
		c.satEntries = c.computeSATLen()
		return
	}

	// Slow path: gather sector ids from the header plus the extension chain
	// into a fresh buffer. Even here we only allocate space for ids (4 bytes
	// each), not the full SAT contents.
	maxIDs := len(c.data)/c.secSize + 1
	buf := make([]byte, 0, 4*masterSATSize)
	for i := 0; i < masterSAT.len(); i++ {
		if masterSAT.at(i) < 0 {
			break
		}
		buf = append(buf, masterSAT.b[4*i:4*i+4]...)
	}
	perSec := c.secSize/4 - 1
	mid := firstMSAT
chain:
	for j := uint32(0); j < nMSAT && mid >= 0; j++ {
		msa, ok := c.sectorIDs(mid)
		if !ok {
			break
		}
		for k := 0; k < perSec; k++ {
			if k >= msa.len() || msa.at(k) < 0 {
				break chain
			}
			buf = append(buf, msa.b[4*k:4*k+4]...)
			if len(buf)/4 > maxIDs {
				break chain // cyclic MSAT chain; stop allocating
			}
		}
		if perSec >= msa.len() {
			break // no next-MSAT pointer available
		}
		mid = msa.at(perSec)
	}
	c.satSecs = int32s{b: buf}
	c.satEntries = c.computeSATLen()
}

// computeSATLen walks satSecs and counts how many SAT entries are actually
// reachable in c.data, stopping at the first sentinel id or sector that is not
// fully present in the file.
func (c *cdf) computeSATLen() int {
	perSec := c.secSize / 4
	total := 0
	for i := 0; i < c.satSecs.len(); i++ {
		sec := c.satSecs.at(i)
		if sec < 0 {
			break
		}
		off := int64(c.secSize) * (1 + int64(sec))
		if off >= int64(len(c.data)) {
			break
		}
		avail := int64(len(c.data)) - off
		if avail >= int64(c.secSize) {
			total += perSec
			continue
		}
		total += int(avail / 4)
		break
	}
	return total
}

// readLong reads a long-sector chain starting at sid. If length > 0 the
// result is truncated to that many bytes. On truncation or any other failure
// it returns whatever sectors were readable.
func (c *cdf) readLong(sid int32, length uint32) []byte {
	// Fast path: when the chain is a single physically contiguous run of
	// sectors (the common case for the directory and summary streams) the data
	// is already laid out sequentially in the input, so return a sub-slice of
	// it instead of allocating a buffer and copying every sector.
	if sid >= 0 {
		maxSec := len(c.data)/c.secSize + 1
		n, s := 0, sid
		contiguous := true
		for s >= 0 {
			if int(s) >= c.satLen() {
				break // SAT truncated; what remains is still contiguous
			}
			n++
			if n > maxSec {
				contiguous = false // cyclic chain; let the slow path guard it
				break
			}
			next := c.satAt(s)
			if next >= 0 && int64(next) != int64(s)+1 {
				contiguous = false
				break
			}
			s = next
		}
		if contiguous {
			off64 := int64(c.secSize) * (1 + int64(sid))
			if off64 >= int64(len(c.data)) {
				return nil
			}
			end64 := min(off64+int64(n)*int64(c.secSize), int64(len(c.data)))
			out := c.data[off64:end64]
			if length > 0 && int(length) < len(out) {
				out = out[:length]
			}
			return out
		}
	}

	// Slow path: gather a fragmented chain into a fresh buffer. Real-world
	// writers (MSI builders, edited Office documents) routinely produce
	// non-contiguous directory and stream chains, so this fallback is required
	// for correct detection on those files.
	maxBytes := len(c.data)
	out := make([]byte, 0, c.secSize)
	for sid >= 0 {
		if int(sid) >= c.satLen() {
			break // SAT truncated; return what we have
		}
		buf, ok := c.sector(sid)
		if !ok {
			break
		}
		out = append(out, buf...)
		if len(out) >= maxBytes {
			break // chain longer than the file: cyclic SAT, stop
		}
		sid = c.satAt(sid)
	}
	if length > 0 && int(length) < len(out) {
		out = out[:length]
	}
	return out
}

// readShort reads a short-sector chain at sid by indexing into the short-stream
// pool. On truncation or if the pool is unavailable it returns whatever was
// readable (possibly nil).
func (c *cdf) readShort(sid int32, length uint32) []byte {
	sst := c.shortStream()
	if sst == nil {
		return nil
	}
	// TODO: anyway to avoid allocating and copying the bytes?
	out := make([]byte, 0, c.shortSecSize)
	for sid >= 0 {
		off := int(sid) * c.shortSecSize
		if off+c.shortSecSize > len(sst) {
			break // short-stream pool truncated or sid out of range
		}
		out = append(out, sst[off:off+c.shortSecSize]...)
		if len(out) >= len(sst) {
			break // chain longer than the pool: cyclic SSAT, stop
		}
		sid = c.ssatAt(sid)
	}
	if length > 0 && int(length) < len(out) {
		out = out[:length]
	}
	return out
}

// readChain dispatches to the long or short reader depending on stream size.
func (c *cdf) readChain(sid int32, length uint32) []byte {
	if length < c.minStdStream && c.rootStreamFirst >= 0 {
		return c.readShort(sid, length)
	}
	return c.readLong(sid, length)
}

// dirLen returns the number of directory entries in dirRaw.
func (c *cdf) dirLen() int { return len(c.dirRaw) / dirEntrySize }

// dirAt decodes the i-th directory entry into *out. Callers must ensure
// i < dirLen(). The UTF-16LE name is decoded into out.name, ASCII-style,
// stopping at the first NUL.
func (c *cdf) dirAt(i int, out *dirEntry) {
	raw := c.dirRaw[i*dirEntrySize:]
	nameLen := min(int(binary.LittleEndian.Uint16(raw[64:])), 64)
	k := uint8(0)
	for j := 0; j < nameLen/2; j++ {
		// Names are ASCII; keep the low byte of each little-endian UTF-16
		// code unit and stop at the first NUL.
		lo, hi := raw[2*j], raw[2*j+1]
		if lo == 0 && hi == 0 {
			break
		}
		out.name[k] = lo
		k++
	}
	out.nameLen = k
	out.typ = raw[66]
	out.streamFirst = readSecID(raw[116:120])
	out.size = binary.LittleEndian.Uint32(raw[120:])
	copy(out.storageUUID[:], raw[80:96])
}

// userStream finds a user stream by name and returns its bytes.
func (c *cdf) userStream(name string) ([]byte, bool) {
	var d dirEntry
	for i, n := 0, c.dirLen(); i < n; i++ {
		c.dirAt(i, &d)
		if d.typ == dirTypeUserStream && string(d.nameBytes()) == name {
			buf := c.readChain(d.streamFirst, d.size)
			if buf == nil {
				return nil, false
			}
			return buf, true
		}
	}
	return nil, false
}

const (
	propIDNameOfApplication = 0x12

	typeMask        = 0x0fff
	typeVector      = 0x1000
	typeStringASCII = 0x1e
	typeStringWide  = 0x1f

	sectionDeclOffset = 0x1c // section declaration in property-set header
)

// summaryAppName parses a (Doc)SummaryInformation stream and returns the
// value of property NameOfApplication (0x12) as printable ASCII, or nil if
// not present or the stream is malformed. This is the only summary property
// the detection logic ever consults.
func summaryAppName(stream []byte) []byte {
	if len(stream) < sectionDeclOffset+20 {
		return nil
	}
	sdOff := binary.LittleEndian.Uint32(stream[sectionDeclOffset+16:])
	if uint64(sdOff)+8 > uint64(len(stream)) {
		return nil
	}
	section := stream[sdOff:]
	shLen := binary.LittleEndian.Uint32(section[0:])
	nProps := binary.LittleEndian.Uint32(section[4:])
	if uint64(shLen) > uint64(len(section)) || nProps > 1<<16 || 8+8*nProps > shLen {
		return nil
	}
	for i := uint32(0); i < nProps; i++ {
		base := 8 + 8*i
		id := binary.LittleEndian.Uint32(section[base:])
		if id != propIDNameOfApplication {
			continue
		}
		off := binary.LittleEndian.Uint32(section[base+4:])
		if uint64(off)+8 > uint64(shLen) {
			return nil
		}
		typ := binary.LittleEndian.Uint32(section[off:])
		if typ&typeVector != 0 {
			return nil
		}
		step := uint32(0)
		switch typ & typeMask {
		case typeStringASCII:
			step = 1
		case typeStringWide:
			step = 2
		default:
			return nil
		}
		slen := binary.LittleEndian.Uint32(section[off+4:])
		start := uint64(off) + 8
		end := start + uint64(slen)*uint64(step)
		if end > uint64(shLen) {
			return nil
		}
		return printableLowBytes(section[start:end], int(step))
	}
	return nil
}

// printableLowBytes copies the printable low byte of each step-byte unit
// in b, stopping at the first NUL.
func printableLowBytes(b []byte, step int) []byte {
	out := make([]byte, 0, len(b)/step)
	for i := 0; i+step <= len(b); i += step {
		c := b[i]
		if c == 0 {
			break
		}
		if c >= 0x20 && c < 0x7f {
			out = append(out, c)
		}
	}
	return out
}

// pattern is a case-insensitive substring → CDFType mapping. Entries are
// tested in order; first match wins. needle is stored upper-cased so it can be
// matched case-insensitively by scan.Bytes.Search with scan.IgnoreCase.
type pattern struct {
	needle []byte
	typ    CDFType
}

// app2type maps NameOfApplication values to CDFTypes.
// Mirrors app2mime[] in libmagic. Needles are upper-cased for case-insensitive
// matching via scan.IgnoreCase.
var app2type = []pattern{
	{[]byte("WORD"), CDFTypeDoc},
	{[]byte("EXCEL"), CDFTypeXls},
	{[]byte("POWERPOINT"), CDFTypePpt},
	{[]byte("ADVANCED INSTALLER"), CDFTypeInstaller},
	{[]byte("INSTALLSHIELD"), CDFTypeInstaller},
	{[]byte("MICROSOFT PATCH COMPILER"), CDFTypeInstaller},
	{[]byte("NANT"), CDFTypeInstaller},
	{[]byte("WINDOWS INSTALLER"), CDFTypeInstaller},
}

// name2type maps directory entry names to CDFTypes.
// Mirrors name2mime[] in libmagic. Needles are upper-cased for case-insensitive
// matching via scan.IgnoreCase.
var name2type = []pattern{
	{[]byte("BOOK"), CDFTypeXls},
	{[]byte("WORKBOOK"), CDFTypeXls},
	{[]byte("WORDDOCUMENT"), CDFTypeDoc},
	{[]byte("POWERPOINT"), CDFTypePpt},
	{[]byte("DIGITALSIGNATURE"), CDFTypeInstaller},
}

// lookupSubstring returns the CDFType for the first entry in t whose needle
// is a case-insensitive substring of v. Mirrors C's strcasestr semantics
// under the C locale. It allocates nothing: scan.IgnoreCase matches the
// upper-cased needle against input of either case.
func lookupSubstring(v []byte, t []pattern) (CDFType, bool) {
	s := scan.Bytes(v)
	for _, p := range t {
		if i, _ := s.Search(p.needle, scan.IgnoreCase); i != -1 {
			return p.typ, true
		}
	}
	return CDFTypeGeneric, false
}

// msiCLSID is the Microsoft Installer root-storage CLSID, in on-disk byte
// order (cdf_directory_t.d_storage_uuid stores two little-endian uint64s).
var msiCLSID = []byte{
	0x84, 0x10, 0x0c, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46,
}

// section is a (directory entry name, type) → CDFType mapping.
type section struct {
	name string
	typ  uint8
	cdf  CDFType
}

// sectionTypes maps distinctive directory entries to CDFTypes — a flattened
// equivalent of sectioninfo[] in libmagic. Used as a fallback when no
// SummaryInformation stream is present. A slice (rather than a map) lets
// lookupSection compare entry names without allocating a string key.
var sectionTypes = []section{
	// libmagic uses application/encrypted, but that is not a registered media type.
	// For now, we skip identifying that and fall-back on CDFTypeGeneric
	// {"EncryptedPackage", dirTypeUserStream, CDFTypeEncrypted},
	// {"EncryptedSummary", dirTypeUserStream, CDFTypeEncrypted},
	{"Book", dirTypeUserStream, CDFTypeXls},
	{"Workbook", dirTypeUserStream, CDFTypeXls},
	{"WordDocument", dirTypeUserStream, CDFTypeDoc},
	{"PowerPoint Document", dirTypeUserStream, CDFTypePpt},
	{"__properties_version1.0", dirTypeUserStream, CDFTypeMsg},
	{"__recip_version1.0_#00000000", dirTypeUserStorage, CDFTypeMsg},
}

// lookupSection returns the CDFType for a directory entry whose name and type
// match a sectionTypes entry exactly. The string(name) == comparison is
// optimized by the compiler to avoid allocating.
func lookupSection(name []byte, typ uint8) (CDFType, bool) {
	for _, s := range sectionTypes {
		if s.typ == typ && string(name) == s.name {
			return s.cdf, true
		}
	}
	return CDFTypeGeneric, false
}
