package pipeline

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
)

var argTypeExamples = map[ArgumentType]string{
	ArgumentTypeString:  "some-value",
	ArgumentTypeInt64:   "13400",
	ArgumentTypeFloat64: "13.4",
	ArgumentTypeSecret:  "some-value",
	ArgumentTypeFile:    "./path/to/file.txt",
	ArgumentTypeFS:      "./path/to/folder",
}

type StdinReader struct {
	out io.Writer
	in  io.Reader
}

func NewStdinReader(in io.Reader, out io.Writer) *StdinReader {
	return &StdinReader{
		out: out,
		in:  in,
	}
}

func (s *StdinReader) Get(arg Argument) (string, error) {
	fmt.Fprintf(s.out, "Argument '%[1]s' requested but not found. Please provide a value for '%[1]s' of type '%s'. Example: '%s': ", arg.Key, arg.Type.String(), argTypeExamples[arg.Type])
	// Prompt for the value via stdin since it was not found
	scanner := bufio.NewScanner(s.in)
	scanner.Scan()

	if err := scanner.Err(); err != nil {
		return "", err
	}

	value := scanner.Text()
	fmt.Fprintf(s.out, "In the future, you can provide this value with the '-arg=%s=%s' argument\n", arg.Key, value)
	return value, nil
}

func (s *StdinReader) GetString(arg Argument) (string, error) {
	val, err := s.Get(arg)
	if err != nil {
		return "", err
	}

	return val, nil
}

func (s *StdinReader) GetDirectoryString(arg Argument) (string, error) {
	val, err := s.Get(arg)
	if err != nil {
		return "", err
	}

	return val, nil
}

func (s *StdinReader) GetInt64(arg Argument) (int64, error) {
	val, err := s.Get(arg)
	if err != nil {
		return 0, err
	}

	return strconv.ParseInt(val, 10, 64)
}

func (s *StdinReader) GetFloat64(arg Argument) (float64, error) {
	val, err := s.Get(arg)
	if err != nil {
		return 0, err
	}

	return strconv.ParseFloat(val, 10)
}

func (s *StdinReader) GetBool(arg Argument) (bool, error) {
	val, err := s.Get(arg)
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(val)
}

func (s *StdinReader) GetFile(arg Argument) (*os.File, error) {
	val, err := s.Get(arg)
	if err != nil {
		return nil, err
	}

	return os.Open(val)
}

func (s *StdinReader) GetDirectory(arg Argument) (fs.FS, error) {
	val, err := s.Get(arg)
	if err != nil {
		return nil, err
	}

	return os.DirFS(val), nil
}

// Since the StdinReader can read any state value, it's better if we assume that if it's being used, then it wasn't found in other reasonable state managers.
func (s *StdinReader) Exists(arg Argument) (bool, error) {
	return false, nil
}
