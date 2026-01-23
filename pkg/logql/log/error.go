package log

var (
	// Possible errors thrown by a log pipeline.
	errJSON             = "JSONParserErr"
	errXML              = "XMLParserErr"
	errLogfmt           = "LogfmtParserErr"
	errSampleExtraction = "SampleExtractionErr"
	errLabelFilter      = "LabelFilterErr"
	errTemplateFormat   = "TemplateFormatErr"
)
