//go:build go1.24

package encoder

// mapValueIsWords is whether a map may be ranged over as a map whose values are the words of its values: the
// maps of Go 1.24 lay a value out at a multiple of a word from its key, so a value of 12 bytes is where one of
// 16 bytes is. The maps before lay the values of a bucket out one after the other, so the size must be the same.
const mapValueIsWords = true
