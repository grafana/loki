package auto

import (
	syslog "github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/rfc3164"
	"github.com/leodido/go-syslog/v4/rfc5424"
)

// SafeMachineOptions wraps each MachineOption so that type-assertion panics
// (from format-specific options applied to the wrong machine type) are
// recovered instead of crashing. This allows generic options from
// syslog.WithMachineOptions to be safely forwarded to both inner machines
// in auto-detect transport parsers.
func SafeMachineOptions(opts []syslog.MachineOption) []syslog.MachineOption {
	safe := make([]syslog.MachineOption, len(opts))
	for i, opt := range opts {
		safe[i] = func(m syslog.Machine) (result syslog.Machine) {
			defer func() {
				if r := recover(); r != nil {
					result = m
				}
			}()
			return opt(m)
		}
	}
	return safe
}

// filterBestEffort returns a copy of opts with any option that enables
// best-effort removed. Detection works by applying each option to a probe
// machine and checking HasBestEffort() before and after.
func filterBestEffort(opts []syslog.MachineOption) []syslog.MachineOption {
	var filtered []syslog.MachineOption
	for _, opt := range opts {
		// Probe with a fresh rfc5424 machine to detect if the option
		// enables best-effort. Use a recover to handle format-specific
		// options that panic on the wrong machine type.
		enablesBE := func() (result bool) {
			defer func() {
				if r := recover(); r != nil {
					result = false // can't probe; assume not best-effort
				}
			}()
			probe := rfc5424.NewMachine()
			opt(probe)
			return probe.HasBestEffort()
		}()
		if !enablesBE {
			filtered = append(filtered, opt)
		}
	}
	return filtered
}

type machine struct {
	rfc3164Opts []syslog.MachineOption
	rfc5424Opts []syslog.MachineOption
	noFallback  bool
	bestEffort  bool
	// Strict machines for primary detection and fallback.
	m3164 syslog.Machine
	m5424 syslog.Machine
	// Best-effort machines, created only when best-effort is enabled.
	// Used as a last resort after both strict machines fail.
	m3164BE syslog.Machine
	m5424BE syslog.Machine
}

// NewMachine returns a syslog.Machine that auto-detects RFC 3164 vs RFC 5424
// format per-message using peek-based heuristics.
func NewMachine(opts ...Option) syslog.Machine {
	m := &machine{}
	for _, opt := range opts {
		opt(m)
	}

	// Build inner machines with all options to detect if any enable best-effort.
	m.m3164 = rfc3164.NewMachine(m.rfc3164Opts...)
	m.m5424 = rfc5424.NewMachine(m.rfc5424Opts...)

	// If options baked best-effort into either inner machine, promote to
	// the auto level and rebuild strict machines without best-effort.
	// This preserves the three-tier strategy: strict primary → strict
	// fallback → best-effort primary.
	if m.m3164.HasBestEffort() || m.m5424.HasBestEffort() {
		m.bestEffort = true
		// BE machines get all original options plus best-effort on both.
		m.m3164BE = m.m3164
		if !m.m3164BE.HasBestEffort() {
			m.m3164BE.WithBestEffort()
		}
		m.m5424BE = m.m5424
		if !m.m5424BE.HasBestEffort() {
			m.m5424BE.WithBestEffort()
		}
		// Strict machines are rebuilt, filtering out best-effort options.
		m.m3164 = rfc3164.NewMachine(filterBestEffort(m.rfc3164Opts)...)
		m.m5424 = rfc5424.NewMachine(filterBestEffort(m.rfc5424Opts)...)
	}

	return m
}

// WithBestEffort enables best-effort mode. Strict parsing is still attempted
// first (primary then fallback) so that detection errors are corrected via
// fallback. Best-effort is only used as a last resort when both strict
// parsers fail, recovering partial data from the peek-chosen parser.
func (m *machine) WithBestEffort() {
	if m.bestEffort {
		return // already enabled (e.g., via option promotion in NewMachine)
	}
	m.bestEffort = true
	m.m3164BE = rfc3164.NewMachine(m.rfc3164Opts...)
	m.m3164BE.WithBestEffort()
	m.m5424BE = rfc5424.NewMachine(m.rfc5424Opts...)
	m.m5424BE.WithBestEffort()
}

// HasBestEffort reports whether best-effort mode is enabled.
func (m *machine) HasBestEffort() bool {
	return m.bestEffort
}

// Parse auto-detects the syslog format of input and delegates to the
// appropriate inner machine.
//
// The strategy is:
//  1. Try the peek-chosen (primary) parser in strict mode.
//  2. If that fails and fallback is enabled, try the other parser strict.
//  3. If both strict parsers fail and best-effort is on, try the
//     peek-chosen parser in best-effort mode for partial recovery.
//  4. On complete failure, return a *ParseError with the raw input bytes.
func (m *machine) Parse(input []byte) (syslog.Message, error) {
	format := detect(input)

	var primary, secondary syslog.Machine
	if format == FormatRFC5424 {
		primary = m.m5424
		secondary = m.m3164
	} else {
		primary = m.m3164
		secondary = m.m5424
	}

	// 1. Try primary in strict mode.
	msg, err := primary.Parse(input)
	if msg != nil {
		return msg, err
	}

	// 2. Try fallback in strict mode.
	if !m.noFallback {
		fbMsg, fbErr := secondary.Parse(input)
		if fbMsg != nil {
			return fbMsg, fbErr
		}
	}

	// 3. Try primary in best-effort mode for partial recovery.
	if m.bestEffort {
		var bePrimary syslog.Machine
		if format == FormatRFC5424 {
			bePrimary = m.m5424BE
		} else {
			bePrimary = m.m3164BE
		}
		beMsg, beErr := bePrimary.Parse(input)
		if beMsg != nil {
			return beMsg, beErr
		}
	}

	return nil, &ParseError{
		Err: err,
		// Defensive copy: input may alias a bufio.Reader internal buffer
		// (e.g., octetcounting scanner's Peek) that is overwritten on the
		// next scan. The copy ensures RawMessage remains valid after Parse returns.
		RawMessage: append([]byte(nil), input...),
	}
}

// compile-time interface check
var _ syslog.Machine = (*machine)(nil)
