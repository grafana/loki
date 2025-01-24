package rfc5424

import (
	"time"
	"fmt"

	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/common"
)

// ColumnPositionTemplate is the template used to communicate the column where errors occur.
var ColumnPositionTemplate = " [col %d]"

const (
	// ErrPrival represents an error in the priority value (PRIVAL) inside the PRI part of the RFC5424 syslog message.
	ErrPrival          = "expecting a priority value in the range 1-191 or equal to 0"
	// ErrPri represents an error in the PRI part of the RFC5424 syslog message.
	ErrPri             = "expecting a priority value within angle brackets"
	// ErrVersion represents an error in the VERSION part of the RFC5424 syslog message.
	ErrVersion         = "expecting a version value in the range 1-999"
	// ErrTimestamp represents an error in the TIMESTAMP part of the RFC5424 syslog message.
	ErrTimestamp       = "expecting a RFC3339MICRO timestamp or a nil value"
	// ErrHostname represents an error in the HOSTNAME part of the RFC5424 syslog message.
	ErrHostname        = "expecting an hostname (from 1 to max 255 US-ASCII characters) or a nil value"
	// ErrAppname represents an error in the APP-NAME part of the RFC5424 syslog message.
	ErrAppname         = "expecting an app-name (from 1 to max 48 US-ASCII characters) or a nil value"
	// ErrProcID represents an error in the PROCID part of the RFC5424 syslog message.
	ErrProcID          = "expecting a procid (from 1 to max 128 US-ASCII characters) or a nil value"
	// ErrMsgID represents an error in the MSGID part of the RFC5424 syslog message.
	ErrMsgID           = "expecting a msgid (from 1 to max 32 US-ASCII characters) or a nil value"
	// ErrStructuredData represents an error in the STRUCTURED DATA part of the RFC5424 syslog message.
	ErrStructuredData  = "expecting a structured data section containing one or more elements (`[id( key=\"value\")*]+`) or a nil value"
	// ErrSdID represents an error regarding the ID of a STRUCTURED DATA element of the RFC5424 syslog message.
	ErrSdID            = "expecting a structured data element id (from 1 to max 32 US-ASCII characters; except `=`, ` `, `]`, and `\"`"
	// ErrSdIDDuplicated represents an error occurring when two STRUCTURED DATA elementes have the same ID in a RFC5424 syslog message.
	ErrSdIDDuplicated  = "duplicate structured data element id"
	// ErrSdParam represents an error regarding a STRUCTURED DATA PARAM of the RFC5424 syslog message.
	ErrSdParam         = "expecting a structured data parameter (`key=\"value\"`, both part from 1 to max 32 US-ASCII characters; key cannot contain `=`, ` `, `]`, and `\"`, while value cannot contain `]`, backslash, and `\"` unless escaped)"
	// ErrMsg represents an error in the MESSAGE part of the RFC5424 syslog message.
	ErrMsg             = "expecting a free-form optional message in UTF-8 (starting with or without BOM)"
	// ErrMsgNotCompliant represents an error in the MESSAGE part of the RFC5424 syslog message if WithCompliatMsg option is on.
	ErrMsgNotCompliant = ErrMsg + " or a free-form optional message in any encoding (starting without BOM)"
	// ErrEscape represents the error for a RFC5424 syslog message occurring when a STRUCTURED DATA PARAM value contains '"', '\', or ']' not escaped.
	ErrEscape          = "expecting chars `]`, `\"`, and `\\` to be escaped within param value"
	// ErrParse represents a general parsing error for a RFC5424 syslog message.
	ErrParse           = "parsing error"
)

// RFC3339MICRO represents the timestamp format that RFC5424 mandates.
const RFC3339MICRO = "2006-01-02T15:04:05.999999Z07:00"

%%{
machine rfc5424;

include common "common.rl";

# unsigned alphabet
alphtype uint8;

action mark {
	m.pb = m.p
}

action markmsg {
	m.msgat = m.p
}

action select_msg_mode {
	fhold;

	if m.compliantMsg {
		fgoto msg_compliant;
	}
	fgoto msg_any;
}

action set_prival {
	output.priority = uint8(common.UnsafeUTF8DecimalCodePointsToInt(m.text()))
	output.prioritySet = true
}

action set_version {
	output.version = uint16(common.UnsafeUTF8DecimalCodePointsToInt(m.text()))
}

action set_timestamp {
	if t, e := time.Parse(RFC3339MICRO, string(m.text())); e != nil {
		m.err = fmt.Errorf("%s [col %d]", e, m.p)
		fhold;
		fgoto fail;
	} else {
		output.timestamp = t
		output.timestampSet = true
	}
}

action set_hostname {
	output.hostname = string(m.text())
}

action set_appname {
	output.appname = string(m.text())
}

action set_procid {
	output.procID = string(m.text())
}

action set_msgid {
	output.msgID = string(m.text())
}

action ini_elements {
	output.structuredData = map[string]map[string]string{}
}

action set_id {
	if _, ok := output.structuredData[string(m.text())]; ok {
		// As per RFC5424 section 6.3.2 SD-ID MUST NOT exist more than once in a message
		m.err = fmt.Errorf(ErrSdIDDuplicated + ColumnPositionTemplate, m.p)
		fhold;
		fgoto fail;
	} else {
		id := string(m.text())
		output.structuredData[id] = map[string]string{}
		output.hasElements = true
		m.currentelem = id
	}
}

action ini_sdparam {
	m.backslashat = []int{}
}

action add_slash {
	m.backslashat = append(m.backslashat, m.p)
}

action set_paramname {
	m.currentparam = string(m.text())
}

action set_paramvalue {
	if output.hasElements {
		// (fixme) > what if SD-PARAM-NAME already exist for the current element (ie., current SD-ID)?

		// Store text
		text := m.text()

		// Strip backslashes only when there are ...
		if len(m.backslashat) > 0 {
			text = common.RemoveBytes(text, m.backslashat, m.pb)
		}
		output.structuredData[m.currentelem][m.currentparam] = string(text)
	}
}

action set_msg {
	output.message = string(m.text())
}

action err_prival {
	m.err = fmt.Errorf(ErrPrival + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_pri {
	m.err = fmt.Errorf(ErrPri + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_version {
	m.err = fmt.Errorf(ErrVersion + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_timestamp {
	m.err = fmt.Errorf(ErrTimestamp + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_hostname {
	m.err = fmt.Errorf(ErrHostname + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_appname {
	m.err = fmt.Errorf(ErrAppname + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_procid {
	m.err = fmt.Errorf(ErrProcID + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_msgid {
	m.err = fmt.Errorf(ErrMsgID + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_structureddata {
	m.err = fmt.Errorf(ErrStructuredData + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_sdid {
	delete(output.structuredData, m.currentelem)
	if len(output.structuredData) == 0 {
		output.hasElements = false
	}
	m.err = fmt.Errorf(ErrSdID + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_sdparam {
	if len(output.structuredData) > 0 {
		delete(output.structuredData[m.currentelem], m.currentparam)
	}
	m.err = fmt.Errorf(ErrSdParam + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_msg {
	// If error encountered within the message rule ...
	if m.msgat > 0 {
		// Save the text until valid (m.p is where the parser has stopped)
		output.message = string(m.data[m.msgat:m.p])
	}

	if m.compliantMsg {
		m.err = fmt.Errorf(ErrMsgNotCompliant + ColumnPositionTemplate, m.p)
	} else {
		m.err = fmt.Errorf(ErrMsg + ColumnPositionTemplate, m.p)
	}

	fhold;
	fgoto fail;
}

action err_escape {
	m.err = fmt.Errorf(ErrEscape + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

action err_parse {
	m.err = fmt.Errorf(ErrParse + ColumnPositionTemplate, m.p)
	fhold;
	fgoto fail;
}

nilvalue = '-';

pri = ('<' prival >mark %from(set_prival) $err(err_prival) '>') @err(err_pri);

version = (nonzerodigit digit{0,2} <err(err_version)) >mark %from(set_version) %eof(set_version) @err(err_version);

timestamp = (nilvalue | (fulldate >mark 'T' fulltime %set_timestamp %err(set_timestamp))) @err(err_timestamp);

hostname = hostnamerange >mark %set_hostname $err(err_hostname);

appname = appnamerange >mark %set_appname $err(err_appname);

procid = procidrange >mark %set_procid $err(err_procid);

msgid = msgidrange >mark %set_msgid $err(err_msgid);

header = (pri version sp timestamp sp hostname sp appname sp procid sp msgid) <>err(err_parse);

# \", \], \\
escapes = (bs >add_slash toescape) $err(err_escape);

# As per section 6.3.3 param value MUST NOT contain '"', '\' and ']', unless they are escaped.
# A backslash '\' followed by none of the this three characters is an invalid escape sequence.
# In this case, treat it as a regular backslash and the following character as a regular character (not altering the invalid sequence).
paramvalue = (utf8charwodelims* escapes*)+ >mark %set_paramvalue;

paramname = sdname >mark %set_paramname;

sdparam = (paramname '=' dq paramvalue dq) >ini_sdparam $err(err_sdparam);

# (note) > finegrained semantics of section 6.3.2 not represented here since not so useful for parsing goal
sdid = sdname >mark %set_id %err(set_id) $err(err_sdid);

sdelement = ('[' sdid (sp sdparam)* ']');

structureddata = nilvalue | sdelement+ >ini_elements $err(err_structureddata);

msg_any := any* >mark >markmsg %set_msg $err(err_msg);

# MSG-ANY = *OCTET ; not starting with BOM
# MSG-UTF8 = BOM *OCTECT ; UTF-8 string as specified in RFC 3629
# MSG = MSG-ANY | MSG-UTF8
msg_compliant := ((bom utf8octets) | (any* - (bom any*))) >mark >markmsg %set_msg $err(err_msg);

msg = any? @select_msg_mode;

fail := (any - [\n\r])* @err{ fgoto main; };

main := header sp structureddata (sp msg)? $err(err_parse);

}%%

%% write data noerror noprefix;

type machine struct {
	data         []byte
	cs           int
	p, pe, eof   int
	pb           int
	err          error
	currentelem  string
	currentparam string
	msgat        int
	backslashat  []int
	bestEffort 	 bool
	compliantMsg bool
}

// NewMachine creates a new FSM able to parse RFC5424 syslog messages.
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

// HasBestEffort tells whether the receiving machine has best effort mode on or off.
func (m *machine) HasBestEffort() bool {
	return m.bestEffort
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

// Parse parses the input byte array as a RFC5424 syslog message.
//
// When a valid RFC5424 syslog message is given it outputs its structured representation.
// If the parsing detects an error it returns it with the position where the error occurred.
//
// It can also partially parse input messages returning a partially valid structured representation
// and the error that stopped the parsing.
func (m *machine) Parse(input []byte) (syslog.Message, error) {
	m.data = input
	m.p = 0
	m.pb = 0
	m.msgat = 0
	m.backslashat = []int{}
	m.pe = len(input)
	m.eof = len(input)
	m.err = nil
	output := &syslogMessage{}

	%% write init;
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
