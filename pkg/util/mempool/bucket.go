package mempool

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/c2h5oh/datasize"
)

type Bucket struct {
	Size     int    // Number of buffers
	Capacity uint64 // Size of a buffer
}

func (b Bucket) Parse(s string) (any, error) {
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return nil, errors.New("bucket must be in format {count}x{bytes}")
	}

	size, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, err
	}

	capacity, err := datasize.ParseString(parts[1])
	if err != nil {
		panic(err.Error())
	}

	return Bucket{
		Size:     size,
		Capacity: uint64(capacity),
	}, nil
}

func (b Bucket) String() string {
	return fmt.Sprintf("%dx%s", b.Size, datasize.ByteSize(b.Capacity).String())
}

type Buckets []Bucket

func (b Buckets) String() string {
	s := make([]string, 0, len(b))
	for i := range b {
		s = append(s, b[i].String())
	}
	return strings.Join(s, ",")
}
