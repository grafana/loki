package file

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"io"
	"os"
)

var summaryThread = 10

type Summary struct {
	lines   []string
	summary string

	t *FileTarget
}

func NewSummary(t *FileTarget) *Summary {
	return &Summary{
		t:     t,
		lines: make([]string, 0),
	}
}

func (s *Summary) readLine(line string) bool {
	if s.summary != "" {
		return true
	}
	s.lines = append(s.lines, line)
	if len(s.lines) <= summaryThread {
		return false
	}

	summaryLines := ""
	for _, line := range s.lines {
		summaryLines += line
	}
	summary := fmt.Sprintf("%x", md5.Sum([]byte(summaryLines)))
	if s.t != nil {
		s.t.summarys.Add(summary, 1)
	}
	s.summary = summary
	return true
}

func (s *Summary) Summary() string {
	return s.summary
}

func summaryFile(path string) (string, error) {
	file, err := os.Open(path)
	defer file.Close()
	summary := NewSummary(nil)
	if nil == err {
		buff := bufio.NewReader(file)
		for {
			line, err := buff.ReadString('\n')
			if err == io.EOF {
				break
			}
			isEnd := summary.readLine(line)
			if isEnd {
				break
			}
		}
	}
	return summary.Summary(), nil
}
