package symbolizer

import (
	"fmt"
	"strings"
)

func New(initialCapacity int, maxSize int) *Symbolizer {
	return &Symbolizer{
		symbols: make(map[string]string, initialCapacity),
		maxSize: maxSize,
	}
}

type Symbolizer struct {
	symbols map[string]string
	maxSize int
}

func (s *Symbolizer) Get(name string) string {
	if value, ok := s.symbols[name]; ok {
		return value
	}
	// Keep memory usage low by discarding a random 1% of symbols if we overflow.
	// We rely on Golang's random map ordering to do this.
	if len(s.symbols) > s.maxSize {
		fmt.Println("symbolizer overflow, discarding random symbols")
		i := 0
		for k := range s.symbols {
			if i > s.maxSize/100 {
				break
			}
			delete(s.symbols, k)
			i++
		}
	}
	s.symbols[name] = strings.Clone(name)
	return s.symbols[name]
}

func (s *Symbolizer) Reset() {
	clear(s.symbols)
}
