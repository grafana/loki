package rfc3164

import (
	// "time"

	"time"

	"github.com/davecgh/go-spew/spew"
)

func output(out interface{}) {
	spew.Config.DisableCapacities = true
	spew.Config.DisablePointerAddresses = true
	spew.Dump(out)
}

func Example() {
	i := []byte(`<13>Dec  2 16:31:03 host app: Test`)
	p := NewParser()
	m, _ := p.Parse(i)
	output(m)
	// Output:
	// (*rfc3164.SyslogMessage)({
	//  Base: (syslog.Base) {
	//   Facility: (*uint8)(1),
	//   Severity: (*uint8)(5),
	//   Priority: (*uint8)(13),
	//   Timestamp: (*time.Time)(0000-12-02 16:31:03 +0000 UTC),
	//   Hostname: (*string)((len=4) "host"),
	//   Appname: (*string)((len=3) "app"),
	//   ProcID: (*string)(<nil>),
	//   MsgID: (*string)(<nil>),
	//   Message: (*string)((len=4) "Test")
	//  }
	// })
}

func Example_currentyear() {
	i := []byte(`<13>Dec  2 16:31:03 host app: Test`)
	p := NewParser(WithYear(CurrentYear{}))
	m, _ := p.Parse(i)
	output(m)
	// Output:
	// (*rfc3164.SyslogMessage)({
	//  Base: (syslog.Base) {
	//   Facility: (*uint8)(1),
	//   Severity: (*uint8)(5),
	//   Priority: (*uint8)(13),
	//   Timestamp: (*time.Time)(2021-12-02 16:31:03 +0000 UTC),
	//   Hostname: (*string)((len=4) "host"),
	//   Appname: (*string)((len=3) "app"),
	//   ProcID: (*string)(<nil>),
	//   MsgID: (*string)(<nil>),
	//   Message: (*string)((len=4) "Test")
	//  }
	// })
}

func Example_withtimezone() {
	cet, _ := time.LoadLocation("CET")
	i := []byte(`<13>Jan 30 02:08:03 host app: Test`)
	p := NewParser(WithTimezone(cet))
	m, _ := p.Parse(i)
	output(m)
	// Output:
	// (*rfc3164.SyslogMessage)({
	//  Base: (syslog.Base) {
	//   Facility: (*uint8)(1),
	//   Severity: (*uint8)(5),
	//   Priority: (*uint8)(13),
	//   Timestamp: (*time.Time)(0000-01-30 03:08:03 +0100 CET),
	//   Hostname: (*string)((len=4) "host"),
	//   Appname: (*string)((len=3) "app"),
	//   ProcID: (*string)(<nil>),
	//   MsgID: (*string)(<nil>),
	//   Message: (*string)((len=4) "Test")
	//  }
	// })
}

func Example_withlocaletimezone() {
	pst, _ := time.LoadLocation("America/New_York")
	i := []byte(`<13>Nov 22 17:09:42 xxx kernel: [118479565.921459] EXT4-fs warning (device sda8): ext4_dx_add_entry:2006: Directory index full!`)
	p := NewParser(WithLocaleTimezone(pst))
	m, _ := p.Parse(i)
	output(m)
	// Output:
	// (*rfc3164.SyslogMessage)({
	//  Base: (syslog.Base) {
	//   Facility: (*uint8)(1),
	//   Severity: (*uint8)(5),
	//   Priority: (*uint8)(13),
	//   Timestamp: (*time.Time)(0000-11-22 17:09:42 -0456 LMT),
	//   Hostname: (*string)((len=3) "xxx"),
	//   Appname: (*string)((len=6) "kernel"),
	//   ProcID: (*string)(<nil>),
	//   MsgID: (*string)(<nil>),
	//   Message: (*string)((len=95) "[118479565.921459] EXT4-fs warning (device sda8): ext4_dx_add_entry:2006: Directory index full!")
	//  }
	// })
}

func Example_withtimezone_and_year() {
	est, _ := time.LoadLocation("EST")
	i := []byte(`<13>Jan 30 02:08:03 host app: Test`)
	p := NewParser(WithTimezone(est), WithYear(Year{YYYY: 1987}))
	m, _ := p.Parse(i)
	output(m)
	// Output:
	// (*rfc3164.SyslogMessage)({
	//  Base: (syslog.Base) {
	//   Facility: (*uint8)(1),
	//   Severity: (*uint8)(5),
	//   Priority: (*uint8)(13),
	//   Timestamp: (*time.Time)(1987-01-29 21:08:03 -0500 EST),
	//   Hostname: (*string)((len=4) "host"),
	//   Appname: (*string)((len=3) "app"),
	//   ProcID: (*string)(<nil>),
	//   MsgID: (*string)(<nil>),
	//   Message: (*string)((len=4) "Test")
	//  }
	// })
}

func Example_besteffort() {
	i := []byte(`<13>Dec  2 16:31:03 -`)
	p := NewParser(WithBestEffort())
	m, _ := p.Parse(i)
	output(m)
	// Output:
	// (*rfc3164.SyslogMessage)({
	//  Base: (syslog.Base) {
	//   Facility: (*uint8)(1),
	//   Severity: (*uint8)(5),
	//   Priority: (*uint8)(13),
	//   Timestamp: (*time.Time)(0000-12-02 16:31:03 +0000 UTC),
	//   Hostname: (*string)(<nil>),
	//   Appname: (*string)(<nil>),
	//   ProcID: (*string)(<nil>),
	//   MsgID: (*string)(<nil>),
	//   Message: (*string)(<nil>)
	//  }
	// })
}

func Example_rfc3339timestamp() {
	i := []byte(`<28>2019-12-02T16:49:23+02:00 host app[23410]: Test`)
	p := NewParser(WithRFC3339())
	m, _ := p.Parse(i)
	output(m)
	// Output:
	// (*rfc3164.SyslogMessage)({
	//  Base: (syslog.Base) {
	//   Facility: (*uint8)(3),
	//   Severity: (*uint8)(4),
	//   Priority: (*uint8)(28),
	//   Timestamp: (*time.Time)(2019-12-02 16:49:23 +0200 +0200),
	//   Hostname: (*string)((len=4) "host"),
	//   Appname: (*string)((len=3) "app"),
	//   ProcID: (*string)((len=5) "23410"),
	//   MsgID: (*string)(<nil>),
	//   Message: (*string)((len=4) "Test")
	//  }
	// })
}

func Example_stamp_also_when_rfc3339() {
	i := []byte(`<28>Dec  2 16:49:23 host app[23410]: Test`)
	p := NewParser(WithYear(Year{YYYY: 2019}), WithRFC3339())
	m, _ := p.Parse(i)
	output(m)
	// Output:
	// (*rfc3164.SyslogMessage)({
	//  Base: (syslog.Base) {
	//   Facility: (*uint8)(3),
	//   Severity: (*uint8)(4),
	//   Priority: (*uint8)(28),
	//   Timestamp: (*time.Time)(2019-12-02 16:49:23 +0000 UTC),
	//   Hostname: (*string)((len=4) "host"),
	//   Appname: (*string)((len=3) "app"),
	//   ProcID: (*string)((len=5) "23410"),
	//   MsgID: (*string)(<nil>),
	//   Message: (*string)((len=4) "Test")
	//  }
	// })
}
