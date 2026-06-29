package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-kit/log"
)

type AnalyzeConfig struct {
	LokiEndpoint string
	LokiQuery    string
	OrgID        string
	From         string
	To           string
	Since        time.Duration

	CellAEndpoint string
	CellBEndpoint string

	ValueComparisonTolerance float64

	Mode              string // "basic" or "deep"
	CorrelationIDsStr string // comma-separated

	Output      string
	Limit       int
	Concurrency int
}

type ParsedAnalyzeConfig struct {
	AnalyzeConfig
	FromTime       time.Time
	ToTime         time.Time
	DeepMode       bool
	CorrelationIDs map[string]struct{}
}

func parseAnalyzeConfig(cfg *AnalyzeConfig) (*ParsedAnalyzeConfig, error) {
	var from, to time.Time

	switch {
	case cfg.Since > 0:
		if cfg.From != "" || cfg.To != "" {
			return nil, fmt.Errorf("--since cannot be used together with --from/--to")
		}
		to = time.Now().UTC()
		from = to.Add(-cfg.Since)
	case cfg.From != "" && cfg.To != "":
		var err error
		from, err = time.Parse(time.RFC3339, cfg.From)
		if err != nil {
			return nil, fmt.Errorf("parsing --from time: %w", err)
		}
		to, err = time.Parse(time.RFC3339, cfg.To)
		if err != nil {
			return nil, fmt.Errorf("parsing --to time: %w", err)
		}
	default:
		return nil, fmt.Errorf("specify either --since or both --from and --to")
	}

	if from.After(to) {
		return nil, fmt.Errorf("--from must be before --to")
	}

	parsed := &ParsedAnalyzeConfig{
		AnalyzeConfig: *cfg,
		FromTime:      from,
		ToTime:        to,
		DeepMode:      cfg.Mode == "deep",
	}

	if cfg.CorrelationIDsStr != "" {
		parsed.CorrelationIDs = make(map[string]struct{})
		for _, id := range strings.Split(cfg.CorrelationIDsStr, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				parsed.CorrelationIDs[id] = struct{}{}
			}
		}
	}

	return parsed, nil
}

var logger log.Logger

func init() {
	logger = log.NewLogfmtLogger(os.Stderr)
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	logger = log.With(logger, "caller", log.DefaultCaller)
}
