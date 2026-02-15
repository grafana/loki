package otelzap

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// fieldExtractorCore copy zapcore.Fields from With method to extraFields list
type fieldExtractorCore struct {
	extraFields *[]zap.Field
}

var _ zapcore.Core = (*fieldExtractorCore)(nil)

// With adds structured context to the Core.
func (fe *fieldExtractorCore) With(fs []zapcore.Field) zapcore.Core {
	*fe.extraFields = append(*fe.extraFields, fs...)
	return nil
}

// Check stub
func (*fieldExtractorCore) Check(zapcore.Entry, *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return nil
}

// Write stub
func (*fieldExtractorCore) Write(zapcore.Entry, []zapcore.Field) error {
	return nil
}

// Sync stub
func (*fieldExtractorCore) Sync() error {
	return nil
}

// Enabled stub
func (*fieldExtractorCore) Enabled(zapcore.Level) bool {
	return false
}
