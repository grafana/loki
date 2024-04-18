//go:build integration

package integration

import (
	"math/rand"
	"time"
)

var randomGenerator *rand.Rand

func init() {
	randomGenerator = rand.New(rand.NewSource(time.Now().UnixNano()))
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randStringRunes() string {
	b := make([]rune, 12)
	for i := range b {
		b[i] = letterRunes[randomGenerator.Intn(len(letterRunes))]
	}
	return string(b)
}
