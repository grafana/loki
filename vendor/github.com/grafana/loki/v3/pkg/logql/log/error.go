package log

var (
	// Possible errors thrown by a log pipeline.
	errJSON             = "JSONParserErr"
	errLogfmt           = "LogfmtParserErr"
	errSampleExtraction = "SampleExtractionErr"
	errLabelFilter      = "LabelFilterErr"
	errTemplateFormat   = "TemplateFormatErr"

	ErrorLabel         = "__error__"
	ErrorDetailsLabel  = "__error_details__"
	PreserveErrorLabel = "__preserve_error__"
)
