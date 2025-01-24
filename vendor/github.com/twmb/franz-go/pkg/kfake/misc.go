package kfake

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

func randFill(slice []byte) {
	randPoolFill(slice)
}

func randBytes(n int) []byte {
	r := make([]byte, n)
	randPoolFill(r)
	return r
}

func randUUID() [16]byte {
	var uuid [16]byte
	randPoolFill(uuid[:])
	return uuid
}

func randStrUUID() string {
	uuid := randUUID()
	return fmt.Sprintf("%x", uuid[:])
}

func hashString(s string) uint64 {
	sum := sha256.Sum256([]byte(s))
	var n uint64
	for i := 0; i < 4; i++ {
		v := binary.BigEndian.Uint64(sum[i*8:])
		n ^= v
	}
	return n
}

var (
	mu         sync.Mutex
	randPool   = make([]byte, 4<<10)
	randPoolAt = len(randPool)
)

func randPoolFill(into []byte) {
	mu.Lock()
	defer mu.Unlock()
	for len(into) != 0 {
		n := copy(into, randPool[randPoolAt:])
		into = into[n:]
		randPoolAt += n
		if randPoolAt == cap(randPool) {
			if _, err := io.ReadFull(rand.Reader, randPool); err != nil {
				panic(fmt.Sprintf("unable to read %d bytes from crypto/rand: %v", len(randPool), err))
			}
			randPoolAt = 0
		}
	}
}
