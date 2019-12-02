[![MIT License](http://img.shields.io/badge/license-MIT-blue.svg?style=for-the-badge)](LICENSE)

**A parser for syslog messages**.

> [Blazing fast](#Performances) RFC5424-compliant parsers

To wrap up, this package provides:

- a RFC5424-compliant parser
- a RFC5424-compliant builder
- a parser which works on streams for syslog with [octet counting](https://tools.ietf.org/html/rfc5425#section-4.3) framing technique
- a parser which works on streams for syslog with [non-transparent](https://tools.ietf.org/html/rfc6587#section-3.4.2) framing technique

This library provides the pieces to parse syslog messages transported following various RFCs.

For example:

- TLS with octet count ([RFC5425](https://tools.ietf.org/html/rfc5425))
- TCP with non-transparent framing or with octet count ([RFC 6587](https://tools.ietf.org/html/rfc6587))
- UDP carrying one message per packet ([RFC5426](https://tools.ietf.org/html/rfc5426))

## Installation

```
go get github.com/influxdata/go-syslog/v2
```

## Docs

[![Documentation](https://img.shields.io/badge/godoc-reference-blue.svg?style=for-the-badge)](http://godoc.org/github.com/influxdata/go-syslog)

The [docs](docs/) directory contains `.dot` files representing the FSM parts of a [RFC5424](https://tools.ietf.org/html/rfc5424) syslog message and also the ones representing the other transport parsers.

## Usage

Suppose you want to parse a given sequence of bytes as a RFC5424 message.

```go
i := []byte(`<165>4 2018-10-11T22:14:15.003Z mymach.it e - 1 [ex@32473 iut="3"] An application event log entry...`)
p := rfc5424.NewParser()
m, e := p.Parse(i, nil)
```

This results in `m` being equal to:

```go
// (*rfc5424.SyslogMessage)({
//  priority: (*uint8)(165),
//  facility: (*uint8)(20),
//  severity: (*uint8)(5),
//  version: (uint16) 4,
//  timestamp: (*time.Time)(2018-10-11 22:14:15.003 +0000 UTC),
//  hostname: (*string)((len=9) "mymach.it"),
//  appname: (*string)((len=1) "e"),
//  procID: (*string)(<nil>),
//  msgID: (*string)((len=1) "1"),
//  structuredData: (*map[string]map[string]string)((len=1) {
//   (string) (len=8) "ex@32473": (map[string]string) (len=1) {
//    (string) (len=3) "iut": (string) (len=1) "3"
//   }
//  }),
//  message: (*string)((len=33) "An application event log entry...")
// })
```

And `e` being equal to `nil`, since the `i` is a perfectly valid RFC5424 message.

### Best effort mode

RFC5424 parser has the ability to perform partial matches (until it can).

With this mode enabled, when the parsing process errors out it returns the message collected until that position, and the error that caused the parser to stop.

Notice that in this modality the output is returned _iff_ it represents a minimally valid message - ie., a message containing almost a priority field in `[1,191]` within angular brackets, followed by a version in `]0,999]`.

Let's look at an example.

```go
bestEffortOn := true
i := []byte("<1>1 A - - - - - -")
p := NewParser()
m, e := p.Parse(i, &bestEffortOn)
```

This results in `m` being equal to the following `SyslogMessage` instance.

```go
// (*rfc5424.SyslogMessage)({
//  priority: (*uint8)(1),
//  facility: (*uint8)(0),
//  severity: (*uint8)(1),
//  version: (uint16) 1,
//  timestamp: (*time.Time)(<nil>),
//  hostname: (*string)(<nil>),
//  appname: (*string)(<nil>),
//  procID: (*string)(<nil>),
//  msgID: (*string)(<nil>),
//  structuredData: (*map[string]map[string]string)(<nil>),
//  message: (*string)(<nil>)
// })
```

And, at the same time, in `e` reporting the error that actually stopped the parser.

```go
expecting a RFC3339MICRO timestamp or a nil value [col 5]
```

Both `m` and `e` have a value since at the column the parser stopped it already was able to construct a minimally valid `SyslogMessage`.

### Builder

This library also provides a builder to construct valid syslog messages.

Notice that its API ignores input values that does not match the grammar.

Let's have a look to an example.

```go
msg := &SyslogMessage{}
msg.SetTimestamp("not a RFC3339MICRO timestamp")
msg.Valid() // Not yet a valid message (try msg.Valid())
msg.SetPriority(191)
msg.SetVersion(1)
msg.Valid() // Now it is minimally valid
```

Printing `msg` you will verify it contains a `nil` timestamp (since an invalid one has been given).

```go
// (*rfc5424.SyslogMessage)({
//  priority: (*uint8)(191),
//  facility: (*uint8)(23),
//  severity: (*uint8)(7),
//  version: (uint16) 1,
//  timestamp: (*time.Time)(<nil>),
//  hostname: (*string)(<nil>),
//  appname: (*string)(<nil>),
//  procID: (*string)(<nil>),
//  msgID: (*string)(<nil>),
//  structuredData: (*map[string]map[string]string)(<nil>),
//  message: (*string)(<nil>)
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

In short, [RFC5425](https://tools.ietf.org/html/rfc5425#section-4.3) and [RFC6587](), aside from the protocol considerations, describe a **transparent framing** technique for syslog messages that uses the **octect counting** technique - ie., the message lenght of the incoming message.

Each syslog message is sent with a prefix representing the number of bytes it is made of.

This [package](./octetcounting) parses messages stream following such rule.

To quickly understand how to use it please have a look at the [example file](./octetcounting/example_test.go).

### Non transparent

The [RFC6587](https://tools.ietf.org/html/rfc6587#section-3.4.2) also describes the **non-transparent framing** transport of syslog messages.

In such case the messages are separated by a trailer, usually a line feed.

This [package](./nontransparent) parses message stream following such [technique](https://tools.ietf.org/html/rfc6587#section-3.4.2).

To quickly understand how to use it please have a look at the [example file](./nontransparent/example_test.go).

Things we do not support:

- trailers other than `LF` or `NUL`
- trailer which length is greater than 1 byte
- trailer change on a frame-by-frame basis

## Performances

To run the benchmark execute the following command.

```bash
make bench
```

On my machine<sup>[1](#mymachine)</sup> this are the results obtained in best effort mode.

```
[no]_empty_input__________________________________-4	30000000       253 ns/op     224 B/op       3 allocs/op
[no]_multiple_syslog_messages_on_multiple_lines___-4	20000000       433 ns/op     304 B/op      12 allocs/op
[no]_impossible_timestamp_________________________-4	10000000      1080 ns/op     528 B/op      11 allocs/op
[no]_malformed_structured_data____________________-4	20000000       552 ns/op     400 B/op      12 allocs/op
[no]_with_duplicated_structured_data_id___________-4	 5000000      1246 ns/op     688 B/op      17 allocs/op
[ok]_minimal______________________________________-4	30000000       264 ns/op     247 B/op       9 allocs/op
[ok]_average_message______________________________-4	 5000000      1984 ns/op    1536 B/op      26 allocs/op
[ok]_complicated_message__________________________-4	 5000000      1644 ns/op    1280 B/op      25 allocs/op
[ok]_very_long_message____________________________-4	 2000000      3826 ns/op    2464 B/op      28 allocs/op
[ok]_all_max_length_and_complete__________________-4	 3000000      2792 ns/op    1888 B/op      28 allocs/op
[ok]_all_max_length_except_structured_data_and_mes-4	 5000000      1830 ns/op     883 B/op      13 allocs/op
[ok]_minimal_with_message_containing_newline______-4	20000000       294 ns/op     250 B/op      10 allocs/op
[ok]_w/o_procid,_w/o_structured_data,_with_message-4	10000000       956 ns/op     364 B/op      11 allocs/op
[ok]_minimal_with_UTF-8_message___________________-4	20000000       586 ns/op     359 B/op      10 allocs/op
[ok]_with_structured_data_id,_w/o_structured_data_-4	10000000       998 ns/op     592 B/op      14 allocs/op
[ok]_with_multiple_structured_data________________-4	 5000000      1538 ns/op    1232 B/op      22 allocs/op
[ok]_with_escaped_backslash_within_structured_data-4	 5000000      1316 ns/op     920 B/op      20 allocs/op
[ok]_with_UTF-8_structured_data_param_value,_with_-4	 5000000      1580 ns/op    1050 B/op      21 allocs/op
```

As you can see it takes:

* ~250ns to parse the smallest legal message

* ~2µs to parse an average legal message

* ~4µs to parse a very long legal message

Other RFC5424 implementations, like this [one](https://github.com/roguelazer/rust-syslog-rfc5424) in Rust, spend 8µs to parse an average legal message.

_TBD: comparation against other golang parsers_.

---

* <a name="mymachine">[1]</a>: Intel Core i7-7600U CPU @ 2.80GHz