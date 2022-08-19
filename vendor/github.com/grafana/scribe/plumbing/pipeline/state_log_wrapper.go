package pipeline

import (
	"io"
	"io/fs"
	"os"

	"github.com/sirupsen/logrus"
)

type StateWriterLogWrapper struct {
	StateWriter
	Log logrus.FieldLogger
}

func (s *StateWriterLogWrapper) SetString(arg Argument, val string) error {
	s.Log.Debugf("Setting string in state for '%s' argument '%s'...", arg.Type, arg.Key)
	err := s.StateWriter.SetString(arg, val)
	if err != nil {
		s.Log.WithError(err).Debugf("Error setting string in state for '%s' argument '%s'", arg.Type, arg.Key)
	}
	return err
}
func (s *StateWriterLogWrapper) SetInt64(arg Argument, val int64) error {
	s.Log.Debugf("Setting int64 in state for '%s' argument '%s'...", arg.Type, arg.Key)
	err := s.StateWriter.SetInt64(arg, val)
	if err != nil {
		s.Log.WithError(err).Debugf("Error setting int64 in state for '%s' argument '%s'", arg.Type, arg.Key)
	}
	return err
}
func (s *StateWriterLogWrapper) SetFloat64(arg Argument, val float64) error {
	s.Log.Debugf("Setting float64 in state for '%s' argument '%s'...", arg.Type, arg.Key)
	err := s.StateWriter.SetFloat64(arg, val)
	if err != nil {
		s.Log.WithError(err).Debugf("Error setting float64 in state for '%s' argument '%s'", arg.Type, arg.Key)
	}

	return err
}
func (s *StateWriterLogWrapper) SetFile(arg Argument, val string) error {
	s.Log.Debugf("Setting file in state for '%s' argument '%s'", arg.Type, arg.Key)
	err := s.StateWriter.SetFile(arg, val)
	if err != nil {
		s.Log.WithError(err).Debugf("Error setting file in state for '%s' argument '%s'", arg.Type, arg.Key)
	}

	return err
}
func (s *StateWriterLogWrapper) SetFileReader(arg Argument, val io.Reader) error {
	s.Log.Debugf("Setting file (using io.Reader) in state for '%s' argument '%s'", arg.Type, arg.Key)
	err := s.StateWriter.SetFileReader(arg, val)
	if err != nil {
		s.Log.WithError(err).Debugf("Error setting file (using io.Reader) in state for '%s' argument '%s'", arg.Type, arg.Key)
	}

	return err
}
func (s *StateWriterLogWrapper) SetDirectory(arg Argument, val string) error {
	s.Log.Debugf("Setting directory in state for '%s' argument '%s'", arg.Type, arg.Key)
	err := s.StateWriter.SetDirectory(arg, val)
	if err != nil {
		s.Log.WithError(err).Debugf("Error setting directory in state for '%s' argument '%s'", arg.Type, arg.Key)
	}

	return err
}

type StateReaderLogWrapper struct {
	StateReader
	Log logrus.FieldLogger
}

func (s *StateReaderLogWrapper) Exists(arg Argument) (bool, error) {
	s.Log.Debugf("Checking state that '%s' argument '%s' exists...", arg.Type, arg.Key)
	v, err := s.StateReader.Exists(arg)
	if err != nil {
		s.Log.Debugf("Error getting state for '%s' key '%s'", arg.Type, arg.Key)
	}

	return v, err
}
func (s *StateReaderLogWrapper) GetString(arg Argument) (string, error) {
	s.Log.Debugf("Getting string from state for '%s' argument '%s'", arg.Type, arg.Key)
	v, err := s.StateReader.GetString(arg)
	if err != nil {
		s.Log.Debugf("Error getting string from state for '%s' key '%s'", arg.Type, arg.Key)
	}

	return v, err
}

func (s *StateReaderLogWrapper) GetInt64(arg Argument) (int64, error) {
	s.Log.Debugf("Getting int64 from state for '%s' argument '%s'", arg.Type, arg.Key)
	v, err := s.StateReader.GetInt64(arg)
	if err != nil {
		s.Log.Debugf("Error getting int64 from state for '%s' key '%s'", arg.Type, arg.Key)
	}

	return v, err
}
func (s *StateReaderLogWrapper) GetFloat64(arg Argument) (float64, error) {
	s.Log.Debugf("Getting float from state for '%s' argument '%s'", arg.Type, arg.Key)
	v, err := s.StateReader.GetFloat64(arg)
	if err != nil {
		s.Log.Debugf("Error getting float64 from state for '%s' key '%s'", arg.Type, arg.Key)
	}

	return v, err
}

func (s *StateReaderLogWrapper) GetFile(arg Argument) (*os.File, error) {
	s.Log.Debugf("Getting file from state for '%s' argument '%s'", arg.Type, arg.Key)
	v, err := s.StateReader.GetFile(arg)
	if err != nil {
		s.Log.Debugf("Error getting file from state for '%s' key '%s'", arg.Type, arg.Key)
	}

	return v, err
}

func (s *StateReaderLogWrapper) GetDirectory(arg Argument) (fs.FS, error) {
	s.Log.Debugf("Getting directory (fs.FS) from state for '%s' argument '%s'", arg.Type, arg.Key)
	v, err := s.StateReader.GetDirectory(arg)
	if err != nil {
		s.Log.Debugf("Error getting directory from state for '%s' key '%s'", arg.Type, arg.Key)
	}

	return v, err
}

func (s *StateReaderLogWrapper) GetDirectoryString(arg Argument) (string, error) {
	s.Log.Debugf("Getting directory (string) from state for '%s' argument '%s'", arg.Type, arg.Key)
	v, err := s.StateReader.GetDirectoryString(arg)
	if err != nil {
		s.Log.Debugf("Error getting int64 state for '%s' key '%s'", arg.Type, arg.Key)
	}

	return v, err
}

type StateHandlerLogWrapper struct {
	*StateReaderLogWrapper
	*StateWriterLogWrapper
}

func StateWriterWithLogs(log logrus.FieldLogger, state StateWriter) *StateWriterLogWrapper {
	return &StateWriterLogWrapper{
		StateWriter: state,
		Log:         log,
	}
}

func StateReaderWithLogs(log logrus.FieldLogger, state StateReader) *StateReaderLogWrapper {
	return &StateReaderLogWrapper{
		StateReader: state,
		Log:         log,
	}
}

func StateHandlerWithLogs(log logrus.FieldLogger, state StateHandler) *StateHandlerLogWrapper {
	return &StateHandlerLogWrapper{
		StateReaderLogWrapper: &StateReaderLogWrapper{
			Log:         log,
			StateReader: state,
		},
		StateWriterLogWrapper: &StateWriterLogWrapper{
			Log:         log,
			StateWriter: state,
		},
	}
}
