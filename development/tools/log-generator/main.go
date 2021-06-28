package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"sync"
	"time"
)

func writeToFile(filename string, interval int, content string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)

	var i int64
	for range ticker.C {
		_, err := f.WriteString(fmt.Sprintf("%v - %v\n", i, content))
		if err != nil {
			return err
		}
		i++
	}

	return nil
}

func main() {
	fmt.Println("Starting log-generator")
	dir := os.Getenv("LOG_DIR")
	if dir == "" {
		log.Fatal("Env var LOG_DIR not set")
	}
	_ = os.Mkdir(dir, 0666)

	var files = []struct {
		name     string
		interval int
		content  string
	}{
		{"secret.log", 1, "secret content"},
		{"dev.log", 1, "dev content"},
		{"production.log", 1, "production content"},
		{"multiline.log", 1, "line1\nline2\nline3\nline4"},
	}

	// We can't use a thing like https://pkg.go.dev/golang.org/x/sync/errgroup
	// because this is run with `go run` without a go.mod. So log.Fatalf is used.
	var wg sync.WaitGroup
	for _, file := range files {
		file := file
		filename := path.Join(dir, file.name)
		wg.Add(1)
		go func() {
			err := writeToFile(filename, file.interval, file.content)
			wg.Done()
			log.Fatalf("Error writing to %v: %v", filename, err)
		}()
	}

	wg.Wait()
}
