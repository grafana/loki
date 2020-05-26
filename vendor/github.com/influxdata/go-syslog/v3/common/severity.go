package common

// SeverityMessages maps severity levels to severity string messages.
var SeverityMessages = map[uint8]string{
	0: "system is unusable",
	1: "action must be taken immediately",
	2: "critical conditions",
	3: "error conditions",
	4: "warning conditions",
	5: "normal but significant condition",
	6: "informational messages",
	7: "debug-level messages",
}

// SeverityLevels maps seveirty levels to severity string levels.
var SeverityLevels = map[uint8]string{
	0: "emergency",
	1: "alert",
	2: "critical",
	3: "error",
	4: "warning",
	5: "notice",
	6: "informational",
	7: "debug",
}

// SeverityLevelsShort maps severity levels to severity short string levels
// as per https://github.com/torvalds/linux/blob/master/tools/include/linux/kern_levels.h and syslog(3).
var SeverityLevelsShort = map[uint8]string{
	0: "emerg",
	1: "alert",
	2: "crit",
	3: "err",
	4: "warning",
	5: "notice",
	6: "info",
	7: "debug",
}