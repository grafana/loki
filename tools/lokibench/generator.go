package lokibench

import (
	"fmt"
	"math/rand"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Queries              []string `yaml:"queries"`
	MinQueryRange        string   `yaml:"min_query_range"`
	MaxQueryRange        string   `yaml:"max_query_range"`
	OldestStartTimestamp string   `yaml:"oldest_start_timestamp"`
	SkipRecentDuration   string   `yaml:"skip_recent_duration"` // e.g. "5m", "1h"
	minRange             time.Duration
	maxRange             time.Duration
	oldestStart          time.Time
	skipDuration         time.Duration // Added this field to store parsed duration
}

func LoadConfig(data []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Parse durations
	var err error
	cfg.minRange, err = time.ParseDuration(cfg.MinQueryRange)
	if err != nil {
		return nil, fmt.Errorf("parsing min_query_range: %w", err)
	}

	cfg.maxRange, err = time.ParseDuration(cfg.MaxQueryRange)
	if err != nil {
		return nil, fmt.Errorf("parsing max_query_range: %w", err)
	}

	// Parse timestamps
	cfg.oldestStart, err = time.Parse(time.RFC3339, cfg.OldestStartTimestamp)
	if err != nil {
		return nil, fmt.Errorf("parsing oldest_start_timestamp: %w", err)
	}

	// Parse skip duration
	cfg.skipDuration, err = time.ParseDuration(cfg.SkipRecentDuration)
	if err != nil {
		cfg.skipDuration = 5 * time.Minute // Default to 5 minutes if not specified or invalid
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if len(c.Queries) == 0 {
		return fmt.Errorf("no queries configured")
	}

	if c.minRange <= 0 {
		return fmt.Errorf("min_query_range must be positive")
	}

	if c.maxRange < c.minRange {
		return fmt.Errorf("max_query_range must be greater than or equal to min_query_range")
	}

	if c.oldestStart.After(time.Now()) {
		return fmt.Errorf("oldest_start_timestamp must be before current time")
	}

	return nil
}

type QueryGenerator struct {
	cfg    *Config
	random *rand.Rand
}

func NewQueryGenerator(cfg *Config) *QueryGenerator {
	return &QueryGenerator{
		cfg:    cfg,
		random: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

type GeneratedQuery struct {
	Query string
	Start time.Time
	End   time.Time
}

func (g *QueryGenerator) Generate() GeneratedQuery {
	// Parse the oldest start time
	oldestStart, err := time.Parse(time.RFC3339, g.cfg.OldestStartTimestamp)
	if err != nil {
		// Handle error or use a reasonable default
		oldestStart = time.Now().Add(-24 * time.Hour)
	}

	// Parse the skip duration
	skipDuration, err := time.ParseDuration(g.cfg.SkipRecentDuration)
	if err != nil {
		// Default to 5 minutes if not specified or invalid
		skipDuration = 5 * time.Minute
	}

	// Calculate the newest possible end time (current time minus skip duration)
	newestPossibleEnd := time.Now().Add(-skipDuration)

	// Generate random time range within bounds
	minRange, _ := time.ParseDuration(g.cfg.MinQueryRange)
	maxRange, _ := time.ParseDuration(g.cfg.MaxQueryRange)

	// Generate random duration between min and max
	// Calculate number of 15m intervals between min and max range
	intervals := int64(maxRange-minRange) / int64(15*time.Minute)
	if intervals < 1 {
		intervals = 1
	}
	// Pick random number of intervals and multiply by 15m
	rangeDuration := minRange + time.Duration(g.random.Int63n(intervals))*15*time.Minute

	// Generate end time between oldest start and newest possible end
	maxStartTime := newestPossibleEnd.Add(-rangeDuration)
	startWindow := maxStartTime.Sub(oldestStart)
	if startWindow < 0 {
		startWindow = 0
	}

	randomOffset := time.Duration(rand.Int63n(int64(startWindow)))
	start := oldestStart.Add(randomOffset)
	end := start.Add(rangeDuration)

	// Pick a random query
	query := g.cfg.Queries[g.random.Intn(len(g.cfg.Queries))]

	return GeneratedQuery{
		Query: query,
		Start: start,
		End:   end,
	}
}
