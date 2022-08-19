package pipeline

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

var (
	ErrorEmptyState = errors.New("state is empty")
	ErrorNotFound   = errors.New("key not found in state")
	ErrorKeyExists  = errors.New("key already exists in state")
	ErrorReadOnly   = errors.New("state is read-only")
)

type StateReader interface {
	Exists(Argument) (bool, error)
	GetString(Argument) (string, error)
	GetInt64(Argument) (int64, error)
	GetFloat64(Argument) (float64, error)
	GetBool(Argument) (bool, error)
	GetFile(Argument) (*os.File, error)
	GetDirectory(Argument) (fs.FS, error)
	GetDirectoryString(Argument) (string, error)
}

type StateWriter interface {
	SetString(Argument, string) error
	SetInt64(Argument, int64) error
	SetFloat64(Argument, float64) error
	SetBool(Argument, bool) error
	SetFile(Argument, string) error
	SetFileReader(Argument, io.Reader) error
	SetDirectory(Argument, string) error
}

type StateHandler interface {
	StateReader
	StateWriter
}

type State struct {
	Handler  StateHandler
	Fallback []StateReader
	Log      logrus.FieldLogger
}

// Exists checks the state to see if an argument exists in it.
// It can return an error in the event of a failure to check the state.
// An error will not be returned if the state could be read and the value was not in it.
// If a value for argument was not found, then false and a nil error is returned.
func (s *State) Exists(arg Argument) (bool, error) {
	exists, err := s.Handler.Exists(arg)
	if err != nil {
		return false, err
	}

	if exists {
		return true, nil
	}

	for _, v := range s.Fallback {
		exists, err := v.Exists(arg)
		if err != nil {
			return false, err
		}
		if exists {
			return exists, nil
		}
	}

	return false, nil
}

// GetString attempts to get the string from the state.
// If there are Fallback readers and the state returned an error, then it will loop through each one, attempting to retrieve the value from the fallback state reader.
// If no fallback reader returns the value, then the original error is returned.
func (s *State) GetString(arg Argument) (string, error) {
	if !ArgumentTypesEqual(arg, ArgumentTypeString, ArgumentTypeSecret) {
		return "", fmt.Errorf("attempted to get string from state for wrong argument type '%s'", arg.Type)
	}

	value, err := s.Handler.GetString(arg)
	if err == nil {
		return value, nil
	}

	for _, v := range s.Fallback {
		s.Log.WithError(err).Debugln("state returned an error; attempting fallback state")
		val, err := v.GetString(arg)
		if err == nil {
			if err := s.SetString(arg, val); err != nil {
				return "", err
			}
			return val, nil
		}

		s.Log.WithError(err).Debugln("fallback state reader returned an error")
	}

	return "", err
}

func (s *State) MustGetString(arg Argument) string {
	val, err := s.GetString(arg)
	if err != nil {
		panic(err)
	}

	return val
}

// GetInt64 attempts to get the int64 from the state.
// If there are Fallback readers and the state returned an error, then it will loop through each one, attempting to retrieve the value from the fallback state reader.
// If no fallback reader returns the value, then the original error is returned.
func (s *State) GetInt64(arg Argument) (int64, error) {
	if !ArgumentTypesEqual(arg, ArgumentTypeInt64) {
		return 0, fmt.Errorf("attempted to get int64 from state for wrong argument type '%s'", arg.Type)
	}

	value, err := s.Handler.GetInt64(arg)
	if err == nil {
		return value, nil
	}

	for _, v := range s.Fallback {
		s.Log.WithError(err).Debugln("state returned an error; attempting fallback state")
		val, err := v.GetInt64(arg)
		if err == nil {
			if err := s.SetInt64(arg, value); err != nil {
				return 0, err
			}
			return val, nil
		}

		s.Log.WithError(err).Debugln("fallback state reader returned an error")
	}

	return 0, err
}

func (s *State) MustGetInt64(arg Argument) int64 {
	val, err := s.GetInt64(arg)
	if err != nil {
		panic(err)
	}

	return val
}

// GetFloat64 attempts to get the int64 from the state.
// If there are Fallback readers and the state returned an error, then it will loop through each one, attempting to retrieve the value from the fallback state reader.
// If no fallback reader returns the value, then the original error is returned.
func (s *State) GetFloat64(arg Argument) (float64, error) {
	if !ArgumentTypesEqual(arg, ArgumentTypeFloat64) {
		return 0.0, fmt.Errorf("attempted to get float64 from state for wrong argument type '%s'", arg.Type)
	}

	s.Log.Debugln("Getting float64 argument", arg.Key, "from state")
	value, err := s.Handler.GetFloat64(arg)
	if err == nil {
		return value, nil
	}

	for _, v := range s.Fallback {
		s.Log.WithError(err).Debugln("state returned an error; attempting fallback state")
		val, err := v.GetFloat64(arg)
		if err == nil {
			if err := s.SetFloat64(arg, val); err != nil {
				return 0, err
			}
			return val, nil
		}

		s.Log.WithError(err).Debugln("fallback state reader returned an error")
	}

	return 0, err
}

func (s *State) MustGetFloat64(arg Argument) float64 {
	val, err := s.GetFloat64(arg)
	if err != nil {
		panic(err)
	}

	return val
}

// GetBool attempts to get the bool from the state.
// If there are Fallback readers and the state returned an error, then it will loop through each one, attempting to retrieve the value from the fallback state reader.
// If no fallback reader returns the value, then the original error is returned.
func (s *State) GetBool(arg Argument) (bool, error) {
	if !ArgumentTypesEqual(arg, ArgumentTypeBool) {
		return false, fmt.Errorf("attempted to get bool from state for wrong argument type '%s'", arg.Type)
	}

	s.Log.Debugln("Getting bool argument", arg.Key, "from state")
	value, err := s.Handler.GetBool(arg)
	if err == nil {
		return value, nil
	}

	for _, v := range s.Fallback {
		s.Log.WithError(err).Debugln("state returned an error; attempting fallback state")
		val, err := v.GetBool(arg)
		if err == nil {
			if err := s.SetBool(arg, val); err != nil {
				return false, err
			}
			return val, nil
		}

		s.Log.WithError(err).Debugln("fallback state reader returned an error")
	}

	return false, err
}

func (s *State) MustGetBool(arg Argument) bool {
	val, err := s.GetBool(arg)
	if err != nil {
		panic(err)
	}

	return val
}

// GetFile attempts to get the file from the state.
// If there are Fallback readers and the state returned an error, then it will loop through each one, attempting to retrieve the value from the fallback state reader.
// If no fallback reader returns the value, then the original error is returned.
func (s *State) GetFile(arg Argument) (*os.File, error) {
	if !ArgumentTypesEqual(arg, ArgumentTypeFile) {
		return nil, fmt.Errorf("attempted to get file from state for wrong argument type '%s'", arg.Type)
	}

	file, err := s.Handler.GetFile(arg)
	if err == nil {
		return file, nil
	}

	for _, v := range s.Fallback {
		s.Log.WithError(err).Debugln("state returned an error; attempting fallback state")
		val, err := v.GetFile(arg)
		if err == nil {
			return val, nil
		}

		s.Log.WithError(err).Debugln("fallback state reader returned an error")
	}

	return nil, err
}

func (s *State) MustGetFile(arg Argument) *os.File {
	val, err := s.GetFile(arg)
	if err != nil {
		panic(err)
	}

	return val
}

// GetDirectory attempts to get the directory from the state.
// If there are Fallback readers and the state returned an error, then it will loop through each one, attempting to retrieve the value from the fallback state reader.
// If no fallback reader returns the value, then the original error is returned.
func (s *State) GetDirectory(arg Argument) (fs.FS, error) {
	if !ArgumentTypesEqual(arg, ArgumentTypeFS, ArgumentTypeUnpackagedFS) {
		return nil, fmt.Errorf("attempted to get directory from state for wrong argument type '%s'", arg.Type)
	}

	dir, err := s.Handler.GetDirectoryString(arg)
	if err == nil {
		return os.DirFS(dir), nil
	}

	for _, v := range s.Fallback {
		s.Log.WithError(err).Debugln("state returned an error; attempting fallback state")
		dir, err := v.GetDirectoryString(arg)
		if err == nil {
			dirAbs, err := filepath.Abs(dir)
			if err != nil {
				return nil, err
			}
			if err := s.SetDirectory(arg, dirAbs); err != nil {
				return nil, err
			}
			return os.DirFS(dir), nil
		}

		s.Log.WithError(err).Debugln("fallback state reader returned an error")
	}

	return nil, err
}

// GetDirectory attempts to get the directory from the state.
// If there are Fallback readers and the state returned an error, then it will loop through each one, attempting to retrieve the value from the fallback state reader.
// If no fallback reader returns the value, then the original error is returned.
func (s *State) GetDirectoryString(arg Argument) (string, error) {
	if !ArgumentTypesEqual(arg, ArgumentTypeFS, ArgumentTypeUnpackagedFS) {
		return "", fmt.Errorf("attempted to get directory from state for wrong argument type '%s'", arg.Type)
	}

	dir, err := s.Handler.GetDirectoryString(arg)
	if err == nil {
		return dir, nil
	}

	for _, v := range s.Fallback {
		s.Log.WithError(err).Debugln("state returned an error; attempting fallback state")
		dir, err := v.GetDirectoryString(arg)
		if err == nil {
			dirAbs, err := filepath.Abs(dir)
			if err != nil {
				return "", fmt.Errorf("error getting absolute path from state value '%s': %w", dir, err)
			}
			if err := s.SetDirectory(arg, dirAbs); err != nil {
				return "", err
			}
			return dir, nil
		}

		s.Log.WithError(err).Debugln("fallback state reader returned an error")
	}

	return "", err
}

func (s *State) MustGetDirectory(arg Argument) fs.FS {
	val, err := s.GetDirectory(arg)
	if err != nil {
		panic(err)
	}

	return val
}

func (s *State) MustGetDirectoryString(arg Argument) string {
	val, err := s.GetDirectoryString(arg)
	if err != nil {
		panic(err)
	}

	return val
}

// SetString attempts to set the string into the state.
func (s *State) SetString(arg Argument, value string) error {
	if !ArgumentTypesEqual(arg, ArgumentTypeString, ArgumentTypeSecret) {
		return fmt.Errorf("attempted to set string in state for wrong argument type '%s'", arg.Type)
	}

	return s.Handler.SetString(arg, value)
}

// SetInt64 attempts to set the int64 into the state.
func (s *State) SetInt64(arg Argument, value int64) error {
	if !ArgumentTypesEqual(arg, ArgumentTypeInt64) {
		return fmt.Errorf("attempted to set int64 in state for wrong argument type '%s'", arg.Type)
	}

	return s.Handler.SetInt64(arg, value)
}

// SetFloat64 attempts to set the float64 into the state.
func (s *State) SetFloat64(arg Argument, value float64) error {
	if !ArgumentTypesEqual(arg, ArgumentTypeFloat64) {
		return fmt.Errorf("attempted to set float64 in state for wrong argument type '%s'", arg.Type)
	}

	return s.Handler.SetFloat64(arg, value)
}

// SetBool attempts to set the bool into the state.
func (s *State) SetBool(arg Argument, value bool) error {
	if !ArgumentTypesEqual(arg, ArgumentTypeBool) {
		return fmt.Errorf("attempted to set bool in state for wrong argument type '%s'", arg.Type)
	}

	return s.Handler.SetBool(arg, value)
}

// SetFile attempts to set the file into the state.
// The "path" argument should be the path to the file to be stored.
func (s *State) SetFile(arg Argument, path string) error {
	if !ArgumentTypesEqual(arg, ArgumentTypeFile) {
		return fmt.Errorf("attempted to set file in state for wrong argument type '%s'", arg.Type)
	}

	return s.Handler.SetFile(arg, path)
}

// SetFileReader attempts to set the reader into the state as a file.
// This is an easy way to go from downloading a file to setting it into the state without having to write it to disk first.
func (s *State) SetFileReader(arg Argument, r io.Reader) error {
	if !ArgumentTypesEqual(arg, ArgumentTypeFile) {
		return fmt.Errorf("attempted to set file in state for wrong argument type '%s'", arg.Type)
	}

	return s.Handler.SetFileReader(arg, r)
}

// SetDirectory attempts to set the directory into the state.
// The "path" argument should be the path to the directory to be stored.
func (s *State) SetDirectory(arg Argument, path string) error {
	if !ArgumentTypesEqual(arg, ArgumentTypeUnpackagedFS, ArgumentTypeFS) {
		return fmt.Errorf("attempted to set folder in state for wrong argument type '%s'", arg.Type)
	}

	return s.Handler.SetDirectory(arg, path)
}
