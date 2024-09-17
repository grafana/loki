[![MIT License](http://img.shields.io/badge/license-MIT-blue.svg?style=for-the-badge)](LICENSE)

**A parser for Syslog messages and transports**.

> [Blazing fast](#Performances) Syslog parsers

_By [@leodido](https://github.com/leodido)_.

_This is the official continuation of influxdata/go-syslog_.

To wrap up, this package provides:

- an [RFC5424-compliant parser and builder](/rfc5424)
- an [RFC3164-compliant parser](/rfc3164) - ie., BSD-syslog messages
- a parser that works on streams for syslog with [octet counting](https://tools.ietf.org/html/rfc5425#section-4.3) framing technique, see [octetcounting](/octetcounting)
- a parser that works on streams for syslog with [non-transparent](https://tools.ietf.org/html/rfc6587#section-3.4.2) framing technique, see [nontransparent](/nontransparent)

This library provides the pieces to parse Syslog messages transported following various RFCs.

For example:

- TLS with octet count ([RFC5425](https://tools.ietf.org/html/rfc5425))
- TCP with non-transparent framing or with octet count ([RFC 6587](https://tools.ietf.org/html/rfc6587))
- UDP carrying one message per packet ([RFC5426](https://tools.ietf.org/html/rfc5426))

## Installation

```
go get github.com/leodido/go-syslog/v4
```

## Docs

[![Documentation](https://img.shields.io/badge/godoc-reference-blue.svg?style=for-the-badge)](http://godoc.org/github.com/leodido/go-syslog)

The [docs](docs/) directory contains `.dot` files representing the finite-state machines (FSMs) implementing the syslog parsers and transports.

## Usage

Suppose you want to parse a given sequence of bytes as a RFC5424 message.

_Notice that the same interface applies for RFC3164. But you can always take a look at the [examples file](./rfc3164/example_test.go)._

```go
i := []byte(`<165>4 2018-10-11T22:14:15.003Z mymach.it e - 1 [ex@32473 iut="3"] An application event log entry...`)
p := rfc5424.NewParser()
m, e := p.Parse(i)
```

This results in `m` being equal to:

```go
// (*rfc5424.SyslogMessage)({
//  Base: (syslog.Base) {
//   Facility: (*uint8)(20),
//   Severity: (*uint8)(5),
//   Priority: (*uint8)(165),
//   Timestamp: (*time.Time)(2018-10-11 22:14:15.003 +0000 UTC),
//   Hostname: (*string)((len=9) "mymach.it"),
//   Appname: (*string)((len=1) "e"),
//   ProcID: (*string)(<nil>),
//   MsgID: (*string)((len=1) "1"),
//   Message: (*string)((len=33) "An application event log entry...")
//  },
//  Version: (uint16) 4,
//  StructuredData: (*map[string]map[string]string)((len=1) {
//   (string) (len=8) "ex@32473": (map[string]string) (len=1) {
//    (string) (len=3) "iut": (string) (len=1) "3"
//   }
//  })
// })
```

And `e` being equal to `nil` since the `i` byte slice contains a perfectly valid RFC5424 message.

### Best effort mode

RFC5424 parser has the ability to perform partial matches (until it can).

With this mode enabled, when the parsing process errors out it returns the message collected until that position, and the error that caused the parser to stop.

Notice that in this modality the output is returned _iff_ it represents a minimally valid message - ie., a message containing almost a priority field in `[1,191]` within angular brackets, followed by a version in `]0,999]` (in the case of RFC5424).

Let's look at an example.

```go
i := []byte("<1>1 A - - - - - -")
p := NewParser(WithBestEffort())
m, e := p.Parse(i)
```

This results in `m` being equal to the following `SyslogMessage` instance.

```go
// (*rfc5424.SyslogMessage)({
//  Base: (syslog.Base) {
//   Facility: (*uint8)(0),
//   Severity: (*uint8)(1),
//   Priority: (*uint8)(1),
//   Timestamp: (*time.Time)(<nil>),
//   Hostname: (*string)(<nil>),
//   Appname: (*string)(<nil>),
//   ProcID: (*string)(<nil>),
//   MsgID: (*string)(<nil>),
//   Message: (*string)(<nil>)
//  },
//  Version: (uint16) 1,
//  StructuredData: (*map[string]map[string]string)(<nil>)
// })
```

And, at the same time, in `e` reporting the error that actually stopped the parser.

```go
// expecting a RFC3339MICRO timestamp or a nil value [col 5]
```

Both `m` and `e` have a value since at the column the parser stopped it already was able to construct a minimally valid RFC5424 `SyslogMessage`.

### Builder

This library also provides a builder to construct valid syslog messages.

Notice that its API ignores input values that does not match the grammar.

Let's have a look to an example.

```go
msg := &rfc5424.SyslogMessage{}
msg.SetTimestamp("not a RFC3339MICRO timestamp")
msg.Valid() // Not yet a valid message (try msg.Valid())
msg.SetPriority(191)
msg.SetVersion(1)
msg.Valid() // Now it is minimally valid
```

Printing `msg` you will verify it contains a `nil` timestamp (since an invalid one has been given).

```go
// (*rfc5424.SyslogMessage)({
//  Base: (syslog.Base) {
//   Facility: (*uint8)(23),
//   Severity: (*uint8)(7),
//   Priority: (*uint8)(191),
//   Timestamp: (*time.Time)(<nil>),
//   Hostname: (*string)(<nil>),
//   Appname: (*string)(<nil>),
//   ProcID: (*string)(<nil>),
//   MsgID: (*string)(<nil>),
//   Message: (*string)(<nil>)
//  },
//  Version: (uint16) 1,
//  StructuredData: (*map[string]map[string]string)(<nil>)
// })
```

Finally you can serialize the message into a string.

```go
str, _ := msg.String()
// <191>1 - - - - - -
```

## Message transfer

Excluding encapsulating one message for packet in packet protocols there are two ways to transfer syslog messages over streams.

The older - ie., the **non-transparent** framing - and the newer one - ie., the **octet counting** framing - which is reliable and has not been seen to cause problems noted with the non-transparent one.

This library provide stream parsers for both.

### Octet counting

In short, [RFC5425](https://tools.ietf.org/html/rfc5425#section-4.3) and [RFC6587](https://tools.ietf.org/html/rfc6587), aside from the protocol considerations, describe a **transparent framing** technique for Syslog messages that uses the **octect counting** technique - ie., the message length of the incoming message.

Each Syslog message is sent with a prefix representing the number of bytes it is made of.

The [octecounting package](./octetcounting) parses messages stream following such rule.

To quickly understand how to use it please have a look at the [example file](./octetcounting/example_test.go).

### Non transparent

The [RFC6587](https://tools.ietf.org/html/rfc6587#section-3.4.2) also describes the **non-transparent framing** transport of syslog messages.

In such case the messages are separated by a trailer, usually a line feed.

The [nontransparent package](./nontransparent) parses message stream following such [technique](https://tools.ietf.org/html/rfc6587#section-3.4.2).

To quickly understand how to use it please have a look at the [example file](./nontransparent/example_test.go).

Things we do not support:

- trailers other than `LF` or `NUL`
- trailers which length is greater than 1 byte
- trailer change on a frame-by-frame basis

## Performances

To run the benchmark execute the following command.

```bash
make bench
```

On my machine<sup>[1](#mymachine)</sup> these are the results obtained paring RFC5424 syslog messages with best effort mode on.

```
[no]_empty_input__________________________________-10  32072733   185.3 ns/op   272 B/op   4 allocs/op
[no]_multiple_syslog_messages_on_multiple_lines___-10  27058381   219.8 ns/op   267 B/op   7 allocs/op
[no]_impossible_timestamp_________________________-10   8732960   683.8 ns/op   555 B/op  12 allocs/op
[no]_malformed_structured_data____________________-10  17997814   335.6 ns/op   499 B/op   8 allocs/op
[no]_with_duplicated_structured_data_id___________-10   9254920   645.7 ns/op   672 B/op  15 allocs/op
[ok]_minimal______________________________________-10  48347473   123.2 ns/op   227 B/op   5 allocs/op
[ok]_average_message______________________________-10   6058492   986.8 ns/op  1344 B/op  20 allocs/op
[ok]_complicated_message__________________________-10   7052536   843.2 ns/op  1232 B/op  23 allocs/op
[ok]_very_long_message____________________________-10   2644068  2279.0 ns/op  2272 B/op  21 allocs/op
[ok]_all_max_length_and_complete__________________-10   3611186  1675.0 ns/op  1848 B/op  27 allocs/op
[ok]_all_max_length_except_structured_data_and_mes-10   5729514  1059.0 ns/op   851 B/op  12 allocs/op
[ok]_minimal_with_message_containing_newline______-10  43165338   142.9 ns/op   230 B/op   6 allocs/op
[ok]_w/o_procid,_w/o_structured_data,_with_message-10  14832892   397.8 ns/op   308 B/op   9 allocs/op
[ok]_minimal_with_UTF-8_message___________________-10  20229313   306.2 ns/op   339 B/op   6 allocs/op
[ok]_minimal_with_UTF-8_message_starting_with_BOM_-10  19721539   306.7 ns/op   355 B/op   6 allocs/op
[ok]_with_structured_data_id,_w/o_structured_data_-10  13860580   435.7 ns/op   538 B/op  10 allocs/op
[ok]_with_multiple_structured_data________________-10   8368731   721.9 ns/op  1173 B/op  15 allocs/op
[ok]_with_escaped_backslash_within_structured_data-10   9730569   632.6 ns/op   864 B/op  16 allocs/op
[ok]_with_UTF-8_structured_data_param_value,_with_-10   8864156   660.6 ns/op   858 B/op  15 allocs/op
```

As you can see it takes:

* ~125ns to parse the smallest legal message

* less than 1µs to parse an average legal message

* ~2µs to parse a very long legal message

Other RFC5424 implementations, like this [one](https://github.com/roguelazer/rust-syslog-rfc5424) in Rust, spend 8µs to parse an average legal message.

---

* <a name="mymachine">[1]</a>: Apple M1 Pro
