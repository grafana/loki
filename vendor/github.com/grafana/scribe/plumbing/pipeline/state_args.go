package pipeline

import (
	"io/fs"
	"os"
	"strconv"

	"github.com/grafana/scribe/plumbing"
)

// ArgMapReader attempts to read state values from the provided "ArgMap".
// The ArgMap is provided by the user by using the '-arg={key}={value}' argument.
// This is typically only used in local executions where some values will not be provided.
type ArgMapReader struct {
	defaults plumbing.ArgMap
}

func NewArgMapReader(defaults plumbing.ArgMap) *ArgMapReader {
	return &ArgMapReader{
		defaults: defaults,
	}
}

func (s *ArgMapReader) GetString(arg Argument) (string, error) {
	val, err := s.defaults.Get(arg.Key)
	if err != nil {
		return "", err
	}

	return val, nil
}

func (s *ArgMapReader) GetInt64(arg Argument) (int64, error) {
	val, err := s.defaults.Get(arg.Key)
	if err != nil {
		return 0, err
	}

	return strconv.ParseInt(val, 10, 64)
}

func (s *ArgMapReader) GetFloat64(arg Argument) (float64, error) {
	val, err := s.defaults.Get(arg.Key)
	if err != nil {
		return 0, err
	}

	return strconv.ParseFloat(val, 10)
}

func (s *ArgMapReader) GetBool(arg Argument) (bool, error) {
	val, err := s.defaults.Get(arg.Key)
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(val)
}

func (s *ArgMapReader) GetFile(arg Argument) (*os.File, error) {
	val, err := s.defaults.Get(arg.Key)
	if err != nil {
		return nil, err
	}

	return os.Open(val)
}

func (s *ArgMapReader) GetDirectory(arg Argument) (fs.FS, error) {
	val, err := s.defaults.Get(arg.Key)
	if err != nil {
		return nil, err
	}

	return os.DirFS(val), nil
}

func (s *ArgMapReader) GetDirectoryString(arg Argument) (string, error) {
	val, err := s.defaults.Get(arg.Key)
	if err != nil {
		return "", err
	}

	return val, nil
}

func (s *ArgMapReader) Exists(arg Argument) (bool, error) {
	// defaults.Get only returns an error if no value was found.
	_, err := s.defaults.Get(arg.Key)
	if err != nil {
		return false, nil
	}

	return true, nil
}
