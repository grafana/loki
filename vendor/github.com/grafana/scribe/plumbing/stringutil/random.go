package stringutil

import "math/rand"

// AllowedCharacters is the list of characters that can be used in the 'Random' function.
var AllowedCharacters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// Random returns a string of a random length using only characters in the package-level variable 'AllowedCharacters'.
func Random(length int) string {
	b := make([]rune, length)
	for i := range b {
		b[i] = AllowedCharacters[rand.Intn(len(AllowedCharacters))]
	}

	return string(b)
}
