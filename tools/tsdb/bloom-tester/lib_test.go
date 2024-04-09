package main

import (
	"bufio"
	"os"
	"testing"
)

const BigFile = "../../../pkg/logql/sketch/testdata/war_peace.txt"

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

			for tokens.Next() {
				tok := tokens.At()
				sbf.TestAndAdd(tok)
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

			for tokens.Next() {
				tok := tokens.At()
				sbf.TestAndAdd(tok)
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

			for tokens.Next() {
				tok := tokens.At()
				sbf.TestAndAdd(tok)
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

			for tokens.Next() {
				tok := tokens.At()
				if !cache.Get(tok) {
					cache.Put(tok)
					sbf.TestAndAdd(tok)
				}
				sbf.TestAndAdd(tok)
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
			for tokens.Next() {
				tok := tokens.At()
				if !cache.Get(tok) {
					cache.Put(tok)
					found := sbf.Test(tok)
					if !found {
						sbf.Add(tok)
					}
				}
				sbf.TestAndAdd(tok)
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
			for tokens.Next() {
				tok := tokens.At()
				tokStr := string(tok)
				_, found := cache[tokStr]
				if !found {
					cache[tokStr] = ""
					f := sbf.Test(tok)
					if !f {
						sbf.Add(tok)
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
