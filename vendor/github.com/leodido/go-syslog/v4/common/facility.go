package common

// Facility maps facility numeric codes to facility string messages.
// As per RFC 5427.
var Facility = map[uint8]string{
	0:  "kernel messages",
	1:  "user-level messages",
	2:  "mail system messages",
	3:  "system daemons messages",
	4:  "authorization messages",
	5:  "messages generated internally by syslogd",
	6:  "line printer subsystem messages",
	7:  "network news subsystem messages",
	8:  "UUCP subsystem messages",
	9:  "clock daemon messages",
	10: "security/authorization messages",
	11: "ftp daemon messages",
	12: "NTP subsystem messages",
	13: "audit messages",
	14: "console messages",
	15: "clock daemon messages",
	16: "local use 0 (local0)",
	17: "local use 1 (local1)",
	18: "local use 2 (local2)",
	19: "local use 3 (local3)",
	20: "local use 4 (local4)",
	21: "local use 5 (local5)",
	22: "local use 6 (local6)",
	23: "local use 7 (local7)",
}

// FacilityKeywords maps facility numeric codes to facility keywords.
// As per RFC 5427.
var FacilityKeywords = map[uint8]string{
	0:  "kern",
	1:  "user",
	2:  "mail",
	3:  "daemon",
	4:  "auth",
	5:  "syslog",
	6:  "lpr",
	7:  "news",
	8:  "uucp",
	9: "cron",
	10: "authpriv",
	11: "ftp",
	12: "ntp",
	13: "security",
	14: "console",
	15: "cron2",
	16: "local0",
	17: "local1",
	18: "local2",
	19: "local3",
	20: "local4",
	21: "local5",
	22: "local6",
	23: "local7",
}
