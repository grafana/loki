package main

import (
	"bufio"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	bt "github.com/grafana/loki/pkg/storage/bloom/v1"
)

const BigFile = "../../../pkg/logql/sketch/testdata/war_peace.txt"

func TestNGrams(t *testing.T) {
	tokenizer := bt.NewNGramTokenizer(2, 4, 0)
	for _, tc := range []struct {
		desc  string
		input string
		exp   []bt.Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []bt.Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []bt.Token{},
		},
		{
			desc:  "two chars",
			input: "ab",
			exp:   []bt.Token{{Key: []byte("ab")}},
		},
		{
			desc:  "three chars",
			input: "abc",
			exp:   []bt.Token{{Key: []byte("ab")}, {Key: []byte("bc")}, {Key: []byte("abc")}},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []bt.Token{{Key: []byte("ab")}, {Key: []byte("bc")}, {Key: []byte("abc")}, {Key: []byte("cd")}, {Key: []byte("bcd")}},
		},
		{
			desc:  "foo",
			input: "日本語",
			exp:   []bt.Token{{Key: []byte("日本")}, {Key: []byte("本語")}, {Key: []byte("日本語")}},
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
		exp   []bt.Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []bt.Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []bt.Token{},
		},
		{
			desc:  "two chars",
			input: "ab",
			exp:   []bt.Token{},
		},
		{
			desc:  "three chars",
			input: "abc",
			exp:   []bt.Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []bt.Token{{Key: []byte("abcd")}},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []bt.Token{{Key: []byte("abcd")}, {Key: []byte("bcde")}},
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
		exp   []bt.Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []bt.Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []bt.Token{},
		},
		{
			desc:  "two chars",
			input: "ab",
			exp:   []bt.Token{},
		},
		{
			desc:  "three chars",
			input: "abc",
			exp:   []bt.Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []bt.Token{},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []bt.Token{},
		},
		{
			desc:  "six chars",
			input: "abcdef",
			exp:   []bt.Token{{Key: []byte("abcdef")}},
		},
		{
			desc:  "seven chars",
			input: "abcdefg",
			exp:   []bt.Token{{Key: []byte("abcdef")}, {Key: []byte("bcdefg")}},
		},
		{
			desc:  "eight chars",
			input: "abcdefgh",
			exp:   []bt.Token{{Key: []byte("abcdef")}, {Key: []byte("bcdefg")}, {Key: []byte("cdefgh")}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func TestNGramsSkip(t *testing.T) {
	twoSkipOne := bt.NewNGramTokenizer(2, 3, 1)
	for _, tc := range []struct {
		desc      string
		tokenizer *bt.NgramTokenizer
		input     string
		exp       []bt.Token
	}{
		{
			desc:      "four chars",
			tokenizer: twoSkipOne,
			input:     "abcd",
			exp:       []bt.Token{{Key: []byte("ab")}, {Key: []byte("cd")}},
		},
		{
			desc:      "special chars",
			tokenizer: twoSkipOne,
			input:     "日本語",
			exp:       []bt.Token{{Key: []byte("日本")}},
		},
		{
			desc:      "multi",
			tokenizer: bt.NewNGramTokenizer(2, 4, 1),
			input:     "abcdefghij",
			exp: []bt.Token{
				{Key: []byte("ab")},
				{Key: []byte("abc")},
				{Key: []byte("cd")},
				{Key: []byte("cde")},
				{Key: []byte("ef")},
				{Key: []byte("efg")},
				{Key: []byte("gh")},
				{Key: []byte("ghi")},
				{Key: []byte("ij")},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tc.tokenizer.Tokens(tc.input))
		})
	}
}

var num = 1000000

func BenchmarkSBFTestAndAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
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
		file, _ := os.Open(BigFile)
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
		file, _ := os.Open(BigFile)
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
		file, _ := os.Open(BigFile)
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

func BenchmarkSBFSeparateTestAndAddWithLRU(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
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

					found := sbf.Test(token.Key)
					if !found {
						sbf.Add(token.Key)
					}
					//sbf.TestAndAdd(token.Key)
				}
			}
		}
	}
}

func BenchmarkSBFSeparateTestAndAddWithLRU5(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		cache := NewLRUCache5(150000)

		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				str := string(token.Key)
				if !cache.Get(str) {
					cache.Put(str)

					found := sbf.Test(token.Key)
					if !found {
						sbf.Add(token.Key)
					}
				}
			}
		}
	}
}

func BenchmarkSBFTestAndAddWithLRU5(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		cache := NewLRUCache5(150000)

		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				str := string(token.Key)
				if !cache.Get(str) {
					cache.Put(str)

					sbf.TestAndAdd(token.Key)
				}
			}
		}
	}
}

func BenchmarkSBFTestAndAddWithByteKeyLRU(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=4skip0_error=1%_indexchunks=false",
			four,
			false,
			onePctError,
		)
		sbf := experiment.bloom()
		cache := NewByteKeyLRUCache(150000)
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {

				array := NewFourByteKeyFromSlice(token.Key)
				if !cache.Get(array) {
					cache.Put(array)
					sbf.TestAndAdd(token.Key)
				}

			}
		}
	}
}

func BenchmarkSBFTestAndAddWithFourByteKeyLRU(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=4skip0_error=1%_indexchunks=false",
			four,
			false,
			onePctError,
		)
		sbf := experiment.bloom()
		cache := NewFourByteKeyLRUCache(150000)
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				if !cache.Get([4]byte(token.Key)) {
					cache.Put([4]byte(token.Key))
					found := sbf.Test(token.Key)
					if !found {
						sbf.Add(token.Key)
					}
					//sbf.TestAndAdd(token.Key)
				}

			}
		}
	}
}

func BenchmarkSBFAddWithLRU(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
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

func BenchmarkSBFSeparateTestAndAddWithLRU1(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		cache := NewLRUCache(150000)
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				str := string(token.Key)
				if !cache.Get(str) {
					cache.Put(str)
					found := sbf.Test(token.Key)
					if !found {
						sbf.Add(token.Key)
					}
					//sbf.Add(token.Key)
				}
			}
		}
	}
}

func BenchmarkSBFSeparateTestAndAddWithMap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open(BigFile)
		defer file.Close()
		scanner := bufio.NewScanner(file)
		experiment := NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)
		sbf := experiment.bloom()
		cache := make(map[string]interface{}, 150000)
		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				str := string(token.Key)

				_, found := cache[str]
				if !found {
					cache[str] = ""
					f := sbf.Test(token.Key)
					if !f {
						sbf.Add(token.Key)
					}

					if len(cache) > 150000 {
						for elem := range cache {
							delete(cache, elem)
						}
					}
				}
			}
		}
	}
}
