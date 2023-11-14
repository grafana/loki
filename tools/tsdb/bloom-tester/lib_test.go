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
