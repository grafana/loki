package main

import (
	"bufio"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNGrams(t *testing.T) {
	tokenizer := newNGramTokenizer(2, 4, 0)
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "two chars",
			input: "ab",
			exp:   []Token{{Key: []byte("ab"), Value: "ab"}},
		},
		{
			desc:  "three chars",
			input: "abc",
			exp:   []Token{{Key: []byte("ab"), Value: "ab"}, {Key: []byte("bc"), Value: "bc"}, {Key: []byte("abc"), Value: "abc"}},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("ab"), Value: "ab"}, {Key: []byte("bc"), Value: "bc"}, {Key: []byte("abc"), Value: "abc"}, {Key: []byte("cd"), Value: "cd"}, {Key: []byte("bcd"), Value: "bcd"}},
		},
		{
			desc:  "foo",
			input: "日本語",
			exp:   []Token{{Key: []byte("日本"), Value: "日本"}, {Key: []byte("本語"), Value: "本語"}, {Key: []byte("日本語"), Value: "日本語"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test4NGrams(t *testing.T) {
	tokenizer := four
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "two chars",
			input: "ab",
			exp:   []Token{},
		},
		{
			desc:  "three chars",
			input: "abc",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("bcde"), Value: "bcde"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test6NGrams(t *testing.T) {
	tokenizer := six
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "two chars",
			input: "ab",
			exp:   []Token{},
		},
		{
			desc:  "three chars",
			input: "abc",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []Token{},
		},
		{
			desc:  "six chars",
			input: "abcdef",
			exp:   []Token{{Key: []byte("abcdef"), Value: "abcdef"}},
		},
		{
			desc:  "seven chars",
			input: "abcdefg",
			exp:   []Token{{Key: []byte("abcdef"), Value: "abcdef"}, {Key: []byte("bcdefg"), Value: "bcdefg"}},
		},
		{
			desc:  "eight chars",
			input: "abcdefgh",
			exp:   []Token{{Key: []byte("abcdef"), Value: "abcdef"}, {Key: []byte("bcdefg"), Value: "bcdefg"}, {Key: []byte("cdefgh"), Value: "cdefgh"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func TestNGramsSkip(t *testing.T) {
	twoSkipOne := newNGramTokenizer(2, 3, 1)
	for _, tc := range []struct {
		desc      string
		tokenizer *ngramTokenizer
		input     string
		exp       []Token
	}{
		{
			desc:      "four chars",
			tokenizer: twoSkipOne,
			input:     "abcd",
			exp:       []Token{{Key: []byte("ab"), Value: "ab"}, {Key: []byte("cd"), Value: "cd"}},
		},
		{
			desc:      "special chars",
			tokenizer: twoSkipOne,
			input:     "日本語",
			exp:       []Token{{Key: []byte("日本"), Value: "日本"}},
		},
		{
			desc:      "multi",
			tokenizer: newNGramTokenizer(2, 4, 1),
			input:     "abcdefghij",
			exp: []Token{
				{Key: []byte("ab"), Value: "ab"},
				{Key: []byte("abc"), Value: "abc"},
				{Key: []byte("cd"), Value: "cd"},
				{Key: []byte("cde"), Value: "cde"},
				{Key: []byte("ef"), Value: "ef"},
				{Key: []byte("efg"), Value: "efg"},
				{Key: []byte("gh"), Value: "gh"},
				{Key: []byte("ghi"), Value: "ghi"},
				{Key: []byte("ij"), Value: "ij"},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tc.tokenizer.Tokens(tc.input))
		})
	}
}

var num = 1000000

func BenchmarkLRU1Put(b *testing.B) {
	cache := NewLRUCache(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
}

func BenchmarkLRU1Get(b *testing.B) {
	cache := NewLRUCache(num)
	for i := 0; i < num; i++ {
		cache.Put(strconv.Itoa(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(strconv.Itoa(i))
	}
}

func BenchmarkLRU2Put(b *testing.B) {
	cache := NewLRUCache2(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
}

func BenchmarkLRU2Get(b *testing.B) {
	cache := NewLRUCache2(num)
	for i := 0; i < num; i++ {
		cache.Put(strconv.Itoa(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(strconv.Itoa(i))
	}
}

func BenchmarkLRU4Put(b *testing.B) {
	cache := NewLRUCache4(num)
	for i := 0; i < b.N; i++ {
		cache.Put([]byte(strconv.Itoa(i)))
	}
}

func BenchmarkLRU4Get(b *testing.B) {
	cache := NewLRUCache4(num)
	for i := 0; i < num; i++ {
		cache.Put([]byte(strconv.Itoa(i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get([]byte(strconv.Itoa(i)))
	}
}

func BenchmarkSBFTestAndAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open("big.txt")
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				sbf.TestAndAdd(token.Key)
			}
		}
	}
}

func BenchmarkSBFAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open("big.txt")
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				sbf.Add(token.Key)
			}
		}
	}
}

func BenchmarkSBFSeparateTestAndAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open("big.txt")
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				found := sbf.Test(token.Key)
				if !found {
					sbf.Add(token.Key)
				}
			}
		}
	}
}

func BenchmarkSBFTestAndAddWithLRU(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open("big.txt")
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		cache := NewLRUCache4(150000)
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				if !cache.Get(token.Key) {
					cache.Put(token.Key)
					sbf.TestAndAdd(token.Key)
				}
			}
		}
	}
}

func BenchmarkSBFAddWithLRU(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open("big.txt")
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		cache := NewLRUCache4(150000)
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				if !cache.Get(token.Key) {
					cache.Put(token.Key)
					sbf.Add(token.Key)
				}
			}
		}
	}
}
