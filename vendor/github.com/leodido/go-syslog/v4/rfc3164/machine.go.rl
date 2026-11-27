package rfc3164

import (
	"fmt"
	"time"

	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/common"
)

var (
	errPrival         = "expecting a priority value in the range 1-191 or equal to 0 [col %d]"
	errPri            = "expecting a priority value within angle brackets [col %d]"
	errTimestamp      = "expecting a Stamp timestamp [col %d]"
	errRFC3339        = "expecting a Stamp or a RFC3339 timestamp [col %d]"
	errMsgCount       = "expecting a message counter (from 1 to max 255 digits) [col %d]"
	errSequence       = "expecting a sequence number (from 1 to max 255 digits) [col %d]"
	errHostname       = "expecting an hostname (from 1 to max 255 US-ASCII characters) [col %d]"
	errTag            = "expecting an alphanumeric tag (max 32 characters) [col %d]"
	errContentStart   = "expecting a content part starting with a non-alphanumeric character [col %d]"
	errContent        = "expecting a content part composed by visible characters only [col %d]"
	errParse          = "parsing error [col %d]"
)

%%{
machine rfc3164;

include common "common.rl";

# unsigned alphabet
alphtype uint8;

action mark {
	m.pb = m.p
}

# Referencing the body label makes Ragel export its entry state.
action set_prival {
	_ = fentry(main::message_body);
	output.priority = uint8(common.UnsafeUTF8DecimalCodePointsToInt(m.text()))
	output.prioritySet = true
}

action set_timestamp {
	if t, e := time.Parse(time.Stamp, string(m.text())); e != nil {
		m.err = fmt.Errorf("%s [col %d]", e, m.p)
		fhold;
		fgoto fail;
	} else {
		if m.timezone != nil {
			t, _ = time.ParseInLocation(time.Stamp, string(m.text()), m.timezone)
		}
		output.timestamp = t.AddDate(m.yyyy, 0, 0)
		if m.loc != nil {
			output.timestamp = output.timestamp.In(m.loc)
		}
		output.timestampSet = true
	}
}

action set_rfc3339 {
	if t, e := time.Parse(time.RFC3339, string(m.text())); e != nil {
		m.err = fmt.Errorf("%s [col %d]", e, m.p)
		fhold;
		fgoto fail;
	} else {
		output.timestamp = t
		output.timestampSet = true
	}
}

action set_msgcount {
	output.msgcount = uint32(common.UnsafeUTF8DecimalCodePointsToInt(m.text()))
	output.msgcountSet = true
}

action set_sequence {
	output.sequence = uint32(common.UnsafeUTF8DecimalCodePointsToInt(m.text()))
	output.sequenceSet = true
}

action set_hostname {
	output.hostname = string(m.text())
}

action set_tag {
	output.tag = string(m.text())
}

action set_content {
	output.content = string(m.text())
}

action set_message {
	output.message = string(m.text())
}

action err_prival {
	m.err = fmt.Errorf(errPrival, m.p)
	fhold;
	fgoto fail;
}

action err_pri {
	m.err = fmt.Errorf(errPri, m.p)
	fhold;
	fgoto fail;
}

action err_timestamp {
	m.err = fmt.Errorf(errTimestamp, m.p)
	fhold;
	fgoto fail;
}

action err_rfc3339 {
	m.err = fmt.Errorf(errRFC3339, m.p)
	fhold;
	fgoto fail;
}

action err_msgcount {
	m.err = fmt.Errorf(errMsgCount, m.p)
	fhold;
	fgoto fail;
}

action err_sequence {
	m.err = fmt.Errorf(errSequence, m.p)
	fhold;
	fgoto fail;
}

action err_hostname {
	m.err = fmt.Errorf(errHostname, m.p)
	fhold;
	fgoto fail;
}

action err_tag {
	m.err = fmt.Errorf(errTag, m.p)
	fhold;
	fgoto fail;
}

action err_contentstart {
	m.err = fmt.Errorf(errContentStart, m.p)
	fhold;
	fgoto fail;
}

action err_content {
	m.err = fmt.Errorf(errContent, m.p)
	fhold;
	fgoto fail;
}

pri = ('<' prival >mark %from(set_prival) $err(err_prival) '>') @err(err_pri);

time = hhmmss (timesecfrac? when { m.secfrac });

timestamp = (datemmm sp datemday sp time) >mark %set_timestamp @err(err_timestamp);
timestamp_lenient = (datemmm sp datemday_lenient sp time) >mark %set_timestamp @err(err_timestamp);

rfc3339 = fulldate >mark 'T' fulltime %set_rfc3339 @err(err_rfc3339);

# note > RFC 3164 says "The Domain Name MUST NOT be included in the HOSTNAME field"
# note > this could mean that the we may need to create and to use a labelrange = graph{1,63} here if we want the parser to be stricter.
hostname = (hostnamerange -- ':') >mark %set_hostname $err(err_hostname);

# Cisco IOS devices sometimes include a "message counter" before the timestamp
# "<189>237: *Jan  8 19:46:03.295..."
msgcountval = (digit*) >mark %set_msgcount @err(err_msgcount);
msgcount = (msgcountval ':' sp*) when { m.msgcount };
# they can also include a "sequence number" after the message counter
# "<189>237: 000104: *Jan  8 19:46:03.295..."
sequenceval = (digit+) >mark %set_sequence @err(err_sequence);
sequence = (sequenceval ':' sp*) when { m.sequence };
# and optionally put a hostname before the timestamp
ciscoHostname = (hostname ':' sp*)? when { m.ciscoHostname };
# and then they prepend a '*' to the timestamp if there is no NTP sync
ciscostar = ('*'?) when { m.msgcount || m.sequence || m.ciscoHostname };
# and they append a colon after the timestamp:
# ...19:46:03.295: ...
ciscocolon = (':'?) when { m.msgcount || m.sequence || m.ciscoHostname };

ciscoextras = msgcount? <: sequence? <: ciscoHostname?;

# Section 4.1.3
# note > alnum{1,32} is too restrictive (eg., no dashes)
# note > see https://tools.ietf.org/html/rfc2234#section-2.1 for an interpretation of "ABNF alphanumeric" as stated by RFC 3164 regarding the tag
# note > while RFC3164 assumes only ABNF alphanumeric process names, many BSD-syslog contains processe names with additional characters (-, _, .)
# note > should be {1,32} but Unifi thinks it can be up to 48 characters
tag = (print -- [ :\[]){1,48} >mark %set_tag @err(err_tag);

# note > we accept HTAB (0x09) in message content even though RFC 3164 technically restricts MSG to VCHAR (%d33-126) and SP (%d32).
# note > this deviation is necessary for interoperability with common syslog implementations that use tabs as field delimiters (e.g., Snare).
visible = print | 0x09 | 0x80..0xFF;

# note > when newline mode is enabled (WithEmbeddedNewlines), we also accept LF (0x0A) and CR (0x0D) inside the message body.
# note > this is needed for octet-counting framing (RFC 5425) where the message length is known upfront,
# note > so embedded newlines are unambiguous and must be preserved.
visible_or_nl = visible | ( 0x0A | 0x0D ) when { m.newline };

# The first not alphanumeric character starts the content (usually containing a PID) part of the message part
contentval = ((visible_or_nl -- [\[\]])*) >mark %set_content @err(err_content);

content = '[' contentval ']' @err(err_contentstart); # todo(leodido) > support ':' and ' ' too. Also they have to match?

mex = visible_or_nl+ >mark %set_message;

msg = (tag content? ':' sp)? mex;

fail := ((any when { m.newline }) | (any - [\n\r]) when { !m.newline })* @err{ fgoto main; };

# note > some BSD syslog implementations insert extra spaces between "PRI", "Timestamp", and "Hostname": although these strictly violate RFC3164, it is useful to be able to parse them
# note > OpenBSD like many other hardware sends syslog messages without hostname
timestamp_value = ((timestamp_lenient when { m.lenientDay }) | timestamp | (rfc3339 when { m.rfc3339 }));

main := pri sp* message_body: (ciscoextras ciscostar timestamp_value ciscocolon sp+ (hostname sp+)? msg '\n'?);

}%%

%% write data noerror noprefix;

type machine struct {
	data          []byte
	cs            int
	p, pe, eof    int
	pb            int
	err           error
	bestEffort    bool
	yyyy          int
	rfc3339       bool
	secfrac       bool
	msgcount      bool
	sequence      bool
	ciscoHostname bool
	lenientDay    bool
	newline       bool
	optionalPriority bool
	loc           *time.Location
	timezone      *time.Location
}

// NewMachine creates a new FSM able to parse RFC3164 syslog messages.
func NewMachine(options ...syslog.MachineOption) syslog.Machine {
	m := &machine{}

	for _, opt := range options {
		opt(m)
	}

	%% access m.;
	%% variable p m.p;
	%% variable pe m.pe;
	%% variable eof m.eof;
	%% variable data m.data;

	return m
}

// WithBestEffort enables best effort mode.
func (m *machine) WithBestEffort() {
	m.bestEffort = true
}

// WithOptionalPriority enables parsing messages without a PRI prefix.
func (m *machine) WithOptionalPriority() {
	m.optionalPriority = true
}

// HasBestEffort tells whether the receiving machine has best effort mode on or off.
func (m *machine) HasBestEffort() bool {
	return m.bestEffort
}

// WithYear sets the year for the Stamp timestamp of the RFC 3164 syslog message.
func (m *machine) WithYear(o YearOperator) {
	m.yyyy = YearOperation{o}.Operate()
}

// WithTimezone sets the time zone for the Stamp timestamp of the RFC 3164 syslog message.
func (m *machine) WithTimezone(loc *time.Location) {
	m.loc = loc
}

// WithLocaleTimezone sets the locale time zone for the Stamp timestamp of the RFC 3164 syslog message.
func (m *machine) WithLocaleTimezone(loc *time.Location) {
	m.timezone = loc
}

// WithRFC3339 enables ability to ALSO match RFC3339 timestamps.
//
// Notice this does not disable the default and correct timestamps - ie., Stamp timestamps.
func (m *machine) WithRFC3339() {
	m.rfc3339 = true
}

// WithSecondFractions enables second fractions for timestamps.
func (m *machine) WithSecondFractions() {
	m.secfrac = true
}

// WithMessageCounter enables parsing of non-standard Cisco IOS logs that include a message counter
func (m *machine) WithMessageCounter() {
	m.msgcount = true
}

// WithSequenceNumber enables parsing of non-standard Cisco IOS logs that include a sequence number.
func (m *machine) WithSequenceNumber() {
	m.sequence = true
}

// WithCiscoHostname enables parsing of non-standard Cisco IOS logs that include a non-standard hostname.
//
// For example:
// `<189>269614: hostname1: Apr 11 10:02:08: %LINEPROTO-5-UPDOWN: Line protocol on Interface GigabitEthernet7/0/34, changed state to up`
func (m *machine) WithCiscoHostname() {
    m.ciscoHostname = true
}

// WithLenientDay enables acceptance of 0-prefixed single-digit days in timestamps (e.g., "Feb 05").
// By default the parser only accepts RFC 3164 compliant timestamps where single-digit days
// are space-padded (e.g., "Feb  5").
func (m *machine) WithLenientDay() {
    m.lenientDay = true
}

// WithEmbeddedNewlines enables acceptance of newline characters (LF, CR) inside the MSG field.
// By default the parser treats newlines as message terminators per RFC 3164.
// Enable this when using octet-counting framing (RFC 5425) where message boundaries
// are determined by the length prefix, making embedded newlines unambiguous.
func (m *machine) WithEmbeddedNewlines() {
    m.newline = true
}

// Err returns the error that occurred on the last call to Parse.
//
// If the result is nil, then the line was parsed successfully.
func (m *machine) Err() error {
	return m.err
}

func (m *machine) text() []byte {
	return m.data[m.pb:m.p]
}

// Parse parses the input byte array as a RFC3164 syslog message.
func (m *machine) Parse(input []byte) (syslog.Message, error) {
	hasPriority := len(input) > 0 && input[0] == '<'
	if !hasPriority && !m.optionalPriority {
		m.err = fmt.Errorf(errPri, 0)
		return nil, m.err
	}
	if !hasPriority {
		msgcount, sequence, ciscoHostname := m.msgcount, m.sequence, m.ciscoHostname
		m.msgcount, m.sequence, m.ciscoHostname = false, false, false
		defer func() {
			m.msgcount, m.sequence, m.ciscoHostname = msgcount, sequence, ciscoHostname
		}()
	}

	m.data = input
	m.p = 0
	m.pb = 0
	m.pe = len(input)
	m.eof = len(input)
	m.err = nil
	output := &syslogMessage{priorityOptional: m.optionalPriority}

	%% write init;
	if !hasPriority {
		m.cs = en_main_message_body
	}
	%% write exec;

	if m.cs < first_final || m.cs == en_fail {
		if m.bestEffort && output.minimal() {
			// An error occurred but partial parsing is on and partial message is minimally valid
			return output.export(), m.err
		}
		return nil, m.err
	}

	return output.export(), nil
}
