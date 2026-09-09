//go:build integration

package integration

import (
	"math/rand"
	"sync"
	"time"
)

var (
	randomGenerator = rand.New(rand.NewSource(time.Now().UnixNano()))
	randomMu        sync.Mutex
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randStringRunes() string {
	b := make([]rune, 12)
	randomMu.Lock()
	defer randomMu.Unlock()
	for i := range b {
		b[i] = letterRunes[randomGenerator.Intn(len(letterRunes))]
	}
	return string(b)
}
