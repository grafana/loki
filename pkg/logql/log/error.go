package log

var (
	// Possible errors thrown by a log pipeline.
	errJSON             = "JSONParserErr"
	errLogfmt           = "LogfmtParserErr"
	errSyslog           = "SyslogParserErr"
	errSampleExtraction = "SampleExtractionErr"
	errLabelFilter      = "LabelFilterErr"
	errTemplateFormat   = "TemplateFormatErr"
)
