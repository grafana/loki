package stages

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
)

// logfmtStage sets the extracted data, timestamp and line for a Logfmt or logfmt like formated logfile.
type logfmtStage struct {
	expression *regexp.Regexp
	logger     log.Logger
}

// newRegexStage creates a newLogfmtStage
func newLogfmtStage(logger log.Logger) (s *logfmtStage, err error) {
	r, err := regexp.Compile(`(?P<key>[^ ]+)=(?:(?:"(?P<text>.*?[^\\])")|(?P<int>[0-9]+)|(?P<float>[0-9]*\.[0-9]+)|(?P<string>[^ ]+?))(?:,? |$)`)
	if err != nil {
		return
	}

	return &logfmtStage{
		expression: r,
		logger:     logger,
	}, nil
}

// logfmtParseMatch parses a regex submatch in to typed values
func logfmtParseMatch(m []string) (key string, value string, parsedValue interface{}, err error) {
	// m's values:
	// 0   1   2    3   4     5
	// all key text int float string
	key = m[1]
	if key == "" {
		err = errors.New("empty key")
		return
	}

	switch {
	case m[2] != "":
		// text
		text := m[2]
		value = fmt.Sprintf(`"%s"`, text)
		parsedValue = strings.Replace(text, "\\\"", "\"", -1)
	case m[3] != "":
		// int
		value = m[3]
		parsedValue, _ = strconv.Atoi(m[3])
	case m[4] != "":
		value = m[4]
		parsedValue, _ = strconv.ParseFloat(m[4], 64)
	case m[5] != "":
		// string
		value = m[5]
		parsedValue = m[5]
	default:
		err = errors.New("unable to match value type")
		return
	}
	return
}

// Process implements Stage
func (s *logfmtStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	matches := s.expression.FindAllStringSubmatch(*entry, -1)
	if len(matches) == 0 {
		level.Debug(s.logger).Log("msg", "unable to parse line with logfmt format")
		return
	}
	out := ""
	for _, match := range matches {
		k, v, pv, err := logfmtParseMatch(match)
		if err != nil {
			level.Debug(s.logger).Log("msg", "unable to parse segment with logfmt format", "err", err, "section", match[0])
			return
		}
		extracted[k] = pv
		out = fmt.Sprintf("%s%s=%v ", out, k, v)
	}
	if len(out) > 0 {
		out = out[0 : len(out)-1]
	}
	if mt, ok := extracted["ts"]; ok {
		mt, ok := mt.(string)
		if ok {
			if tt, err := time.Parse("2006-01-02T15:04:05-07:00", mt); err == nil {
				*t = tt
			}
		}
	}
	*entry = out
}

// Name implements Stage
func (s *logfmtStage) Name() string {
	return StageTypeLogfmt
}
