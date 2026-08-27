// Package log provides building blocks for constructing go-kit loggers
// used across dskit-based services.
//
// It includes constructors for logfmt and JSON loggers (NewGoKit), a
// Level type integrated with command-line flags, buffered and
// rate-limited loggers, and a global logger accessor.
//
// When logging values that may originate from untrusted sources, use
// DropUnsafeChars or EscapeUnsafeChars to sanitize control and
// formatting characters before they reach the encoder. These helpers
// help prevent log injection, terminal escape sequence injection, and
// similar attacks while preserving compatibility with both logfmt and
// JSON logging.
package log
