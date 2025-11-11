# RFC 3164 Syslog Parser

This package implements a parser for RFC3164 (BSD syslog) messages with extensions for common non-standard formats.

## Overview

RFC3164 is the original syslog protocol specification, commonly known as BSD syslog. Unlike [RFC5424](../rfc5424), RFC3164 has a loosely defined format, making it challenging to parse reliably. This parser handles the standard format plus common variations.

## Standard RFC3164 Format

```
<PRI>TIMESTAMP HOSTNAME TAG[PROCID]: MESSAGE
```

Example:

```
<34>Oct 11 22:14:15 mymachine su: 'su root' failed for lonvick on /dev/pts/8
```

## Basic Usage

```go
import "github.com/leodido/go-syslog/v4/rfc3164"

// Create a parser
p := rfc3164.NewParser()

// Parse a message
msg, err := p.Parse([]byte("<34>Oct 11 22:14:15 mymachine su: 'su root' failed"))
if err != nil {
    log.Fatal(err)
}

// Access parsed fields
m := msg.(*rfc3164.SyslogMessage)
fmt.Printf("Priority: %d\n", *m.Priority)
fmt.Printf("Hostname: %s\n", *m.Hostname)
fmt.Printf("Message: %s\n", *m.Message)
```

## Parser Options

### Timezone Configuration

By default, timestamps without timezone information use UTC. You can specify a different timezone:

```go
loc, _ := time.LoadLocation("America/New_York")
p := rfc3164.NewParser(
    rfc3164.WithTimezone(loc),
)
```

### Year Specification

RFC3164 timestamps don't include the year. Specify it explicitly:

```go
p := rfc3164.NewParser(
    rfc3164.WithYear(rfc3164.Year{YYYY: 2024}),
)
```

### Best Effort Mode

Parse partial messages when complete parsing fails:

```go
p := rfc3164.NewParser(
    rfc3164.WithBestEffort(),
)
```

### Cisco IOS Support

Cisco IOS devices send non-standard syslog messages with additional fields before the timestamp. This parser supports these extensions.

```
<PRI>[msgcount:] [sequence:] [hostname:] [*]TIMESTAMP: MESSAGE
```

Example:

```
<189>237: 000485: router1: *Jan 8 19:46:03.295: %LINEPROTO-5-UPDOWN: Line protocol on Interface Loopback100, changed state to up
```

#### Enabling Cisco Support

```go
import (
    "github.com/leodido/go-syslog/v4/rfc3164"
    "github.com/leodido/go-syslog/v4/rfc3164/ciscoios"
)

// Parse all Cisco IOS components
p := rfc3164.NewParser(
    rfc3164.WithCiscoIOSComponents(ciscoios.All),
)

// Selectively disable components
p := rfc3164.NewParser(
    rfc3164.WithCiscoIOSComponents(
        ciscoios.DisableSequenceNumber | ciscoios.DisableHostname,
    ),
)
```

#### Cisco Components

For Cisco IOS configuration details, see the `WithCiscoIOSComponents()` function documentation.

| Component        | Description             | Cisco Command                                                        |
| ---------------- | ----------------------- | -------------------------------------------------------------------- |
| Message Counter  | Remote logging counter  | Enabled by default; disable with `no logging message-counter syslog` |
| Service Sequence | Global message sequence | `service sequence-numbers`                                           |
| Hostname         | Origin hostname         | `logging origin-id hostname`                                         |
| Milliseconds     | Timestamp precision     | `service timestamps log datetime msec`                               |
| Asterisk         | NTP sync indicator      | Appears when NTP not synchronized                                    |

#### Configuration Matching Requirement

**Important**: Your parser configuration must match your Cisco device configuration. The parser cannot auto-detect which components are present because they share similar formats (mostly digits followed by colon).

What happens when there's a mismatch?

```go
// Device sends both message counter and sequence number: <189>237: 000485: *Jan 8 19:46:03.295: ...
// Parser configured for message counter only (mismatch):
p := rfc3164.NewParser(
    rfc3164.WithCiscoIOSComponents(ciscoios.DisableSequenceNumber),
)
// Result: Parse error "expecting a sequence number (from 1 to max 255 digits) [col 10]"
// Parser found digits where it expects timestamp, indicating sequence parsing should be enabled.
```

##### Cisco Device Configuration

```cisco
conf t
! Enable message counter (default for remote logging)
logging host 10.0.0.10

! Add service sequence numbers
service sequence-numbers

! Add origin hostname
logging origin-id hostname

! Enable millisecond timestamps
service timestamps log datetime msec localtime

! Recommended: Enable NTP to remove asterisk
ntp server <your-ntp-server>
```

## Known Limitations

### Ambiguities

RFC3164 has an underspecified format, leading to parsing challenges:

- No standard field delimiters beyond whitespace
- Hostname and tag can be ambiguous
- No year in timestamps
- No timezone specification in basic format

### Cisco IOS Limitations

**Component Ordering**: When Cisco components are selectively disabled on the device but the parser expects them, parsing will fail or produce incorrect results. Always match your parser configuration to your device configuration.

**Structured Data**: Cisco IOS messages with RFC5424-style structured data blocks (from `logging host X session-id` or `sequence-num-session`) are not currently supported. See issue #35 for details.
