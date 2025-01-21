package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/grafana/loki/v3/tools/lokibench"
)

type config struct {
	primaryURL   string
	secondaryURL string
	configFile   string
	queriesCount int
	queryDelay   time.Duration
}

func main() {
	cfg := &config{}
	flag.StringVar(&cfg.primaryURL, "primary", "", "Primary Loki deployment URL (query-frontend)")
	flag.StringVar(&cfg.secondaryURL, "secondary", "", "Secondary Loki deployment URL (query-frontend)")
	flag.StringVar(&cfg.configFile, "config", "", "Path to YAML config file")
	flag.IntVar(&cfg.queriesCount, "queries", 10, "Number of queries to run")
	flag.DurationVar(&cfg.queryDelay, "query-delay", 5*time.Second, "Delay between queries")
	flag.Parse()

	if cfg.primaryURL == "" {
		log.Fatal("primary URL must be set")
	}
	if cfg.secondaryURL == "" {
		log.Fatal("secondary URL must be set")
	}
	if cfg.configFile == "" {
		log.Fatal("config file must be set")
	}

	if err := runBenchmark(cfg); err != nil {
		log.Fatal(err)
	}
}

func runBenchmark(cfg *config) error {
	// Load query config
	data, err := os.ReadFile(cfg.configFile)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	queryCfg, err := lokibench.LoadConfig(data)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	generator := lokibench.NewQueryGenerator(queryCfg)
	ctx := context.Background()

	// Run queries
	i := 0
	for cfg.queriesCount == 0 || i < cfg.queriesCount {
		query := generator.Generate()
		fmt.Printf("\nRunning query %d: %s\n", i+1, query.Query)
		fmt.Printf("Time range: %s to %s\n", query.Start.Format(time.RFC3339), query.End.Format(time.RFC3339))
		fmt.Printf("Duration: %s\n", query.End.Sub(query.Start))

		if err := runComparison(ctx, cfg, query); err != nil {
			return fmt.Errorf("running comparison: %w", err)
		}
		i++

		// Print accumulated stats every 10 iterations
		if i%10 == 0 {
			fmt.Printf("\n=== Accumulated stats after %d queries ===\n", i)
			fmt.Println(accumulator.String())
		}

		// Add delay before next query if not the last iteration
		if cfg.queriesCount == 0 || i < cfg.queriesCount {
			time.Sleep(cfg.queryDelay)
		}
	}

	// Print final stats
	if i%10 != 0 {
		fmt.Printf("\n=== Final accumulated stats after %d queries ===\n", i)
		fmt.Println(accumulator.String())
	}

	return nil
}

var accumulator *lokibench.StatsAccumulator = lokibench.NewStatsAccumulator()

func runComparison(ctx context.Context, cfg *config, query lokibench.GeneratedQuery) error {
	primary, err := executeQuery(ctx, cfg.primaryURL, query)
	if err != nil {
		return fmt.Errorf("querying primary: %w", err)
	}

	secondary, err := executeQuery(ctx, cfg.secondaryURL, query)
	if err != nil {
		return fmt.Errorf("querying secondary: %w", err)
	}

	diff := lokibench.CompareStats(primary, secondary)
	accumulator.Add(primary, secondary)
	fmt.Println(diff.String())
	return nil
}

func executeQuery(ctx context.Context, baseURL string, query lokibench.GeneratedQuery) (*lokibench.Response, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	u.Path = "/loki/api/v1/query_range"

	q := u.Query()
	q.Set("query", query.Query)
	q.Set("start", query.Start.Format(time.RFC3339))
	q.Set("end", query.End.Format(time.RFC3339))
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Scope-OrgID", "29")
	req.Header.Set("Cache-control", "no-Cache")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result lokibench.Response
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &result, nil
}
