package decoder

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"unicode"
	"unicode/utf8"
)

// structKeys finds the field of a struct which an object key is decoded into, as encoding/json does:
// the field whose key is the same, or else the first field whose key is the same by case folding.
//
// The fields are in a table keyed by their folded keys: the fields of a folded key are in one entry, so that
// a key is looked up once, whether it matches a field exactly or by folding. A key is looked up by two words:
// its first eight bytes, and its last eight bytes, which overlap the first ones for a key shorter than 16 bytes.
// These are all the bytes of a key of up to 16 bytes, so such a key is compared by its length and the two words
// only; a longer one is compared by the words between them as well. The words of a key of ASCII are folded in
// place, eight bytes at a time, and only a key which is not ASCII is folded rune by rune.
type structKeys struct {
	entries []keyEntry
	shift   uint
	// lengths has the bit of the length of every folded key ( 63 for the longer keys ):
	// a key of another length matches no field, which is known without a lookup.
	lengths uint64
	// exact is a table of the keys which are not ASCII, by their bytes as they are ( see findExact ), and
	// exactLengths has the bit of the length of every such key, as lengths does for the folded keys.
	exact        []keyEntry
	exactShift   uint
	exactLengths uint64
	// foldRunes has a bit for every rune which is not ASCII and which folds to a rune of a folded key, by the
	// low byte of the rune ( see mayFold ).
	foldRunes [4]uint64
	// foldLeads has a bit for the first byte of the encoding of every rune of foldRunes ( see leadMayFold ).
	foldLeads [4]uint64
	// hasRuneError is set if a key has utf8.RuneError, which a byte of an object key which is not valid UTF-8
	// is replaced by.
	hasRuneError bool
}

type keyEntry struct {
	// short is the word of the folded key if it is of less than 8 bytes, none of which is nul, and its field
	// is the only one: a short key of ASCII whose folded word is the same is of that field, whatever its case.
	// It is noShortKey for any other entry, which no such word is.
	short uint64
	// w0 and w1 are the words of the folded key, and n its length.
	w0, w1 uint64
	n      int
	folded []byte
	// unique is the field of the folded key if it is the only one, which a key matches whichever its case.
	unique *structFieldSet
	// fields are the fields of the folded key in the order of the struct: the first one is the field of
	// a key which matches none of them exactly.
	fields []*structFieldSet
}

// hasLength reports whether a key of n bytes may match a field.
func (k *structKeys) hasLength(n int) bool {
	return k.lengths&(1<<(uint(min(n, 63))&63)) != 0
}

// newStructKeys makes the table of the fields, which are in the order of the struct.
func newStructKeys(fields []*structFieldSet) *structKeys {
	// the table is at most half full, so that a lookup ends after a few entries.
	size := 8
	for size < 2*len(fields) {
		size *= 2
	}
	k := &structKeys{
		entries: make([]keyEntry, size),
		shift:   uint(64 - bits.TrailingZeros(uint(size))),
	}
	for _, field := range fields {
		folded := appendFoldedKey(nil, []byte(field.key))
		if e := k.find(folded); e != nil {
			e.fields = append(e.fields, field)
			e.unique = nil
			continue
		}
		k.insert(folded, field)
	}
	k.makeExact(fields)
	k.makeFoldRunes(fields)
	for i := range k.entries {
		e := &k.entries[i]
		e.short = noShortKey
		if e.unique != nil && e.n < 8 && !bytes.Contains(e.folded, []byte{0}) {
			e.short = e.w0
		}
	}
	return k
}

// makeExact makes the table of the keys which are not ASCII, by their bytes as they are. A key which is not
// valid UTF-8 is not in it: an object key which is not valid UTF-8 is decoded with its bytes replaced first.
func (k *structKeys) makeExact(fields []*structFieldSet) {
	var keys []*structFieldSet
	for _, field := range fields {
		if !isASCII(field.key) && utf8.ValidString(field.key) {
			keys = append(keys, field)
		}
	}
	if len(keys) == 0 {
		return
	}
	size := 8
	for size < 2*len(keys) {
		size *= 2
	}
	k.exact = make([]keyEntry, size)
	k.exactShift = uint(64 - bits.TrailingZeros(uint(size)))
	for _, field := range keys {
		key := []byte(field.key)
		w0, w1 := keyWords(key)
		h0, h1 := hashWords(w0, w1, len(key))
		mask := len(k.exact) - 1
		for i := indexOf(h0, h1, k.exactShift); ; i = (i + 1) & mask {
			if k.exact[i].unique == nil {
				k.exact[i] = keyEntry{w0: w0, w1: w1, n: len(key), folded: key, unique: field}
				k.exactLengths |= 1 << (uint(min(len(key), 63)) & 63)
				break
			}
		}
	}
}

// findExact returns the field whose key is the same as the key, which is not ASCII and is in a buffer which
// has room for a word after it, or nil. w0 and w1 are the words of the key as keyWords makes them.
func (k *structKeys) findExact(key []byte, w0, w1 uint64) *structFieldSet {
	n := len(key)
	mask := len(k.exact) - 1
	h0, h1 := hashWords(w0, w1, n)
	for i := indexOf(h0, h1, k.exactShift); ; i = (i + 1) & mask {
		e := &k.exact[i]
		if e.unique == nil {
			return nil
		}
		if e.n == n && e.w0 == w0 && e.w1 == w1 && (n <= 16 || equalMiddleWords(key, e.folded)) {
			return e.unique
		}
	}
}

// hasExactLength reports whether a key of n bytes which is not ASCII may be the same as the key of a field.
func (k *structKeys) hasExactLength(n int) bool {
	return k.exactLengths&(1<<(uint(min(n, 63))&63)) != 0
}

// equalMiddleWords reports whether two keys of the same length of more than 16 bytes, whose first and last eight
// bytes are known to be the same, have the same words between them.
func equalMiddleWords(a, b []byte) bool {
	n := len(a)
	if n != len(b) {
		return false
	}
	for i := 8; i < n-8; i += 8 {
		if binary.LittleEndian.Uint64(a[i:]) != binary.LittleEndian.Uint64(b[i:]) {
			return false
		}
	}
	return true
}

// makeFoldRunes sets the bits of the runes which are not ASCII and fold to a rune of a key: the runes of the
// fold orbits of the runes of the keys, which include the Kelvin sign for K and the long s for S.
func (k *structKeys) makeFoldRunes(fields []*structFieldSet) {
	for _, field := range fields {
		for _, r := range field.key {
			if r == utf8.RuneError {
				k.hasRuneError = true
			}
			for f := r; ; {
				if f >= utf8.RuneSelf {
					k.foldRunes[f&0xff>>6] |= 1 << (uint(f) & 63)
					lead := utf8.AppendRune(nil, f)[0]
					k.foldLeads[lead>>6] |= 1 << (lead & 63)
				}
				if f = unicode.SimpleFold(f); f == r {
					break
				}
			}
		}
	}
}

// mayFold reports whether a key which is not ASCII may be of a field by case folding: every rune of it which
// is not ASCII folds to a rune of a key, which is told by the low byte of the rune. The low byte is taken from
// the last two bytes of the encoding of the rune, which is not decoded. A key which is not valid UTF-8 has its
// invalid bytes replaced by utf8.RuneError, so it may be of a field only if a key has utf8.RuneError: then
// the runes are decoded.
func (k *structKeys) mayFold(key []byte) bool {
	if k.hasRuneError {
		return k.mayFoldDecoded(key)
	}
	n := len(key)
	for i := 0; i < n; {
		c := key[i]
		if c < utf8.RuneSelf {
			i++
			continue
		}
		size := 2
		switch {
		case c < 0xc0:
			return false // a continuation byte without a first byte: not valid UTF-8
		case c >= 0xf0:
			size = 4
		case c >= 0xe0:
			size = 3
		}
		if i+size > n {
			return false
		}
		low := key[i+size-2]&3<<6 | key[i+size-1]&0x3f
		if k.foldRunes[low>>6]&(1<<(low&63)) == 0 {
			return false
		}
		i += size
	}
	return true
}

// leadMayFold reports whether the first byte of the key which is not ASCII, if it is in the first word of the
// key, w0, may be the first byte of a rune which folds to a rune of a key, and else true. It is the cheapest
// check, which needs no load: most keys which are of no field are told so by it.
func (k *structKeys) leadMayFold(w0 uint64) bool {
	high := w0 & msb
	if high == 0 || k.hasRuneError {
		return true
	}
	lead := byte(w0 >> (uint(bits.TrailingZeros64(high)) - 7))
	return k.foldLeads[lead>>6]&(1<<(lead&63)) != 0
}

// firstRuneMayFold reports whether the first rune of the key which is not ASCII may fold to a rune of a key, if
// that rune begins in the first word of the key, w0, and else true: it tells mayFold for most keys, without a
// call. The low byte of the rune is taken from its bytes as mayFold does, and a key which may not be valid
// UTF-8 is left to mayFold.
func (k *structKeys) firstRuneMayFold(key []byte, w0 uint64) bool {
	high := w0 & msb
	if high == 0 || k.hasRuneError {
		return true
	}
	i := bits.TrailingZeros64(high) / 8
	c := key[i]
	size := 2
	switch {
	case c < 0xc0:
		return true
	case c >= 0xf0:
		size = 4
	case c >= 0xe0:
		size = 3
	}
	if i+size > len(key) {
		return true
	}
	low := key[i+size-2]&3<<6 | key[i+size-1]&0x3f
	return k.foldRunes[low>>6]&(1<<(low&63)) != 0
}

// mayFoldDecoded is mayFold by the decoded runes.
func (k *structKeys) mayFoldDecoded(key []byte) bool {
	for i := 0; i < len(key); {
		if key[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRune(key[i:])
		if k.foldRunes[r&0xff>>6]&(1<<(uint(r)&63)) == 0 {
			return false
		}
		i += size
	}
	return true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// noShortKey is the short word of an entry which a short key is not found by: a word of ASCII has no byte of
// 0x80 or more.
const noShortKey = ^uint64(0)

// keyWords returns the words of a key: its first eight bytes and its last eight bytes, as little-endian
// numbers, which are zero where the key is shorter. The capacity of the key is read past its length when it
// has room, which is the rest of the buffer of the input.
func keyWords(key []byte) (uint64, uint64) {
	n := len(key)
	switch {
	case n >= 8:
		return binary.LittleEndian.Uint64(key), binary.LittleEndian.Uint64(key[n-8:])
	case cap(key) >= 8:
		return binary.LittleEndian.Uint64(key[:8]) & (1<<(8*uint(n)) - 1), 0
	}
	var w uint64
	for i := n - 1; i >= 0; i-- {
		w = w<<8 | uint64(key[i])
	}
	return w, 0
}

// index returns the first entry to look at for a key whose words are w0 and w1, with the bit 5 of their bytes
// set by hashWords: a letter of either case has the same index, so the index of a key is made from its bytes
// before they are folded. The product spreads the keys, which differ in a few bits.
func (k *structKeys) index(w0, w1 uint64) int {
	return indexOf(w0, w1, k.shift)
}

func indexOf(w0, w1 uint64, shift uint) int {
	h := (w0 ^ bits.RotateLeft64(w1, 29)) * 0x9E3779B97F4A7C15
	return int(h >> shift)
}

// hashWords returns the words of a key of n bytes, or of its folded key, which index takes: the bit 5 of their
// bytes set, which folding a letter doesn't change, and the bytes after the key zero.
func hashWords(w0, w1 uint64, n int) (uint64, uint64) {
	if n < 8 {
		return (w0 | bit5) & (1<<(uint(n)*8&63) - 1), 0
	}
	return w0 | bit5, w1 | bit5
}

func (k *structKeys) insert(folded []byte, field *structFieldSet) {
	w0, w1 := keyWords(folded)
	mask := len(k.entries) - 1
	for i := k.index(hashWords(w0, w1, len(folded))); ; i = (i + 1) & mask {
		if k.entries[i].fields == nil {
			k.entries[i] = keyEntry{w0: w0, w1: w1, n: len(folded), folded: folded, unique: field, fields: []*structFieldSet{field}}
			k.lengths |= 1 << uint(min(len(folded), 63))
			return
		}
	}
}

// find returns the entry of the folded key, or nil.
func (k *structKeys) find(folded []byte) *keyEntry {
	w0, w1 := keyWords(folded)
	n := len(folded)
	mask := len(k.entries) - 1
	for i := k.index(hashWords(w0, w1, n)); ; i = (i + 1) & mask {
		e := &k.entries[i]
		if e.fields == nil {
			return nil
		}
		if e.n == n && e.w0 == w0 && e.w1 == w1 && (n <= 16 || string(e.folded) == string(folded)) {
			return e
		}
	}
}

// field returns the field of the key among the fields of its folded key: the one whose key is the same,
// or the first one.
func (e *keyEntry) field(key []byte) *structFieldSet {
	if len(e.fields) == 1 {
		return e.fields[0]
	}
	for _, field := range e.fields {
		if field.key == string(key) {
			return field
		}
	}
	return e.fields[0]
}

// findASCII returns the field of a key of ASCII without an escape, whose words are w0 and w1, or nil.
func (k *structKeys) findASCII(key []byte, w0, w1 uint64) *structFieldSet {
	n := len(key)
	start := k.index(hashWords(w0, w1, n))
	w0, w1 = foldASCIIWord(w0), foldASCIIWord(w1)
	mask := len(k.entries) - 1
	for i := start; ; i = (i + 1) & mask {
		e := &k.entries[i]
		if e.fields == nil {
			return nil
		}
		if e.n == n && e.w0 == w0 && e.w1 == w1 && (n <= 16 || equalFoldedASCII(key, e.folded)) {
			if len(e.fields) == 1 {
				return e.fields[0]
			}
			return e.field(key)
		}
	}
}

// foldASCIIWord folds the bytes of a word of ASCII as encoding/json does: a lower case letter to upper case.
func foldASCIIWord(w uint64) uint64 {
	// the bytes are less than 0x80: adding to one never carries to the next.
	ge := w + (0x80-'a')*lsb   // 0x80 in the bytes from 'a'
	gt := w + (0x80-'z'-1)*lsb // 0x80 in the bytes after 'z'
	lower := (ge &^ gt) & msb  // 0x80 in the lower case letters
	return w &^ (lower >> 2)   // clear 0x20 of them
}

// equalFoldedASCII reports whether the key of ASCII, of more than 16 bytes, folds to folded, whose first and
// last eight bytes are known to be the ones of the key folded: the words between them are compared.
func equalFoldedASCII(key, folded []byte) bool {
	n := len(key)
	if n != len(folded) {
		return false
	}
	for i := 8; i < n-8; i += 8 {
		if foldASCIIWord(binary.LittleEndian.Uint64(key[i:])) != binary.LittleEndian.Uint64(folded[i:]) {
			return false
		}
	}
	return true
}

// lookup returns the field of the key, or nil, and the key, which is decoded in place if it is escaped.
func (k *structKeys) lookup(key []byte, info stringInfo) (*structFieldSet, []byte) {
	if info.firstEscape < 0 && !info.nonASCII {
		if !k.hasLength(len(key)) {
			return nil, key
		}
		w0, w1 := keyWords(key)
		return k.findASCII(key, w0, w1), key
	}
	key = decodeLiteral(key, info)
	var arr [64]byte
	if e := k.find(appendFoldedKey(arr[:0], key)); e != nil {
		return e.field(key), key
	}
	return nil, key
}

// appendFoldedKey appends the key folded as encoding/json folds a key to compare it case insensitively:
// a lower case ASCII letter to upper case, and any other rune to the smallest rune which folds to it.
func appendFoldedKey(out, key []byte) []byte {
	for i := 0; i < len(key); {
		if c := key[i]; c < utf8.RuneSelf {
			if 'a' <= c && c <= 'z' {
				c -= 'a' - 'A'
			}
			out = append(out, c)
			i++
			continue
		}
		r, n := utf8.DecodeRune(key[i:])
		out = utf8.AppendRune(out, foldRune(r))
		i += n
	}
	return out
}

// foldRune returns the smallest rune of the fold set of r.
func foldRune(r rune) rune {
	for {
		r2 := unicode.SimpleFold(r)
		if r2 <= r {
			return r2
		}
		r = r2
	}
}
