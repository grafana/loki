package testutils

import (
	"math/rand"
	"time"
)

var randomGenerator *rand.Rand

func InitRandom() {
	randomGenerator = rand.New(rand.NewSource(time.Now().UnixNano()))
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandName() string {
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[randomGenerator.Intn(len(letters))] //#nosec G404 -- Generating random test data, fine.
	}
	return string(b)
}
