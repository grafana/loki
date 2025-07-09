package query

import (
	"context"
	stdErrors "errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/logcli/print"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/storage"
	chunk "github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/util/cfg"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/marshal"
	"github.com/grafana/loki/v3/pkg/validation"
)

const schemaConfigFilename = "schemaconfig"

// Query contains all necessary fields to execute instant and range queries and print the results.
type Query struct {
	QueryString            string
	Start                  time.Time
	End                    time.Time
	Limit                  int
	BatchSize              int
	Forward                bool
	Step                   time.Duration
	Interval               time.Duration
	Quiet                  bool
	NoLabels               bool
	IgnoreLabelsKey        []string
	ShowLabelsKey          []string
	IncludeCommonLabels    bool
	FixedLabelsLen         int
	ColoredOutput          bool
	LocalConfig            string
	FetchSchemaFromStorage bool
	SchemaStore            string

	// Parallelization parameters.

	// The duration of each part/job.
	ParallelDuration time.Duration

	// Number of workers to start.
	ParallelMaxWorkers int

	// Path prefix of the name for each part file.
	// The idea for this is to allow the user to download many different queries at the same
	// time, and/or give a directory for the part files to be placed.
	PartPathPrefix string

	// By default (false value), if the part file has finished downloading, and another job with
	// the same filename is run, it will skip the completed files. This will remove the completed
	// files as each worker gets to that part file, so the part will be downloaded again.
	OverwriteCompleted bool

	// If true, the part files will be read in order, and the data will be output to stdout.
	MergeParts bool

	// If MergeParts is false, this parameter has no effect, part files will be kept.
	// Otherwise, if this is true, the part files will not be deleted once they have been merged.
	KeepParts bool
}

// DoQuery executes the query and prints out the results
func (q *Query) DoQuery(c client.Client, out output.LogOutput, statistics bool) {
	if q.LocalConfig != "" {
		orgID := c.GetOrgID()
		if orgID == "" {
			orgID = "fake"
		}
		if err := q.DoLocalQuery(out, statistics, orgID, q.FetchSchemaFromStorage); err != nil {
			log.Fatalf("Query failed: %+v", err)
		}
		return
	}

	d := q.resultsDirection()

	var resp *loghttp.QueryResponse
	var err error

	var partFile *PartFile
	if q.PartPathPrefix != "" {
		var shouldSkip bool
		partFile, shouldSkip = q.createPartFile()

		// createPartFile will return true if the part file exists and
		// OverwriteCompleted is false, therefor, we should exit the function
		// here because we have nothing to do.
		if shouldSkip {
			return
		}
	}

	if partFile != nil {
		defer partFile.Close()
		out = out.WithWriter(partFile)
	}

	result := print.NewQueryResultPrinter(q.ShowLabelsKey, q.IgnoreLabelsKey, q.Quiet, q.FixedLabelsLen, q.Forward, q.IncludeCommonLabels)

	if q.isInstant() {
		resp, err = c.Query(q.QueryString, q.Limit, q.Start, d, q.Quiet)
		if err != nil {
			log.Fatalf("Query failed: %+v", err)
		}
		if statistics {
			result.PrintStats(resp.Data.Statistics)
		}
		_, _ = result.PrintResult(resp.Data.Result, out, nil)
	} else {
		unlimited := q.Limit == 0

		if q.Limit < q.BatchSize && !unlimited {
			q.BatchSize = q.Limit
		}
		resultLength := 0
		total := 0
		start := q.Start
		end := q.End
		var lastEntry []*loghttp.Entry
		for total < q.Limit || unlimited {
			bs := q.BatchSize
			// We want to truncate the batch size if the remaining number
			// of items needed to reach the limit is less than the batch size
			// unless the query has no limit, ie limit==0.
			if q.Limit-total < q.BatchSize && !unlimited {
				// Truncated batchsize is q.Limit - total, however we add to this
				// the length of the overlap from the last query to make sure we get the
				// correct amount of new logs knowing there will be some overlapping logs returned.
				bs = q.Limit - total + len(lastEntry)
			}
			resp, err = c.QueryRange(q.QueryString, bs, start, end, d, q.Step, q.Interval, q.Quiet)
			if err != nil {
				log.Fatalf("Query failed: %+v", err)
			}

			if statistics {
				result.PrintStats(resp.Data.Statistics)
			}

			resultLength, lastEntry = result.PrintResult(resp.Data.Result, out, lastEntry)
			// Was not a log stream query, or no results, no more batching
			if resultLength <= 0 {
				break
			}
			// Also no result, wouldn't expect to hit this.
			if len(lastEntry) == 0 {
				break
			}
			// Can only happen if all the results return in one request
			if resultLength == q.Limit {
				break
			}
			if len(lastEntry) >= q.BatchSize {
				log.Fatalf("Invalid batch size %v, the next query will have %v overlapping entries "+
					"(there will always be 1 overlapping entry but Loki allows multiple entries to have "+
					"the same timestamp, so when a batch ends in this scenario the next query will include "+
					"all the overlapping entries again).  Please increase your batch size to at least %v to account "+
					"for overlapping entryes\n", q.BatchSize, len(lastEntry), len(lastEntry)+1)
			}

			// Batching works by taking the timestamp of the last query and using it in the next query,
			// because Loki supports multiple entries with the same timestamp it's possible for a batch to have
			// fallen in the middle of a list of entries for the same time, so to make sure we get all entries
			// we start the query on the same time as the last entry from the last batch, and then we keep this last
			// entry and remove the duplicate when printing the results.
			// Because of this duplicate entry, we have to subtract it here from the total for each batch
			// to get the desired limit.
			total += resultLength
			// Based on the query direction we either set the start or end for the next query.
			// If there are multiple entries in `lastEntry` they have to have the same timestamp so we can pick just the first
			if q.Forward {
				start = lastEntry[0].Timestamp
			} else {
				// The end timestamp is exclusive on a backward query, so to make sure we get back an overlapping result
				// fudge the timestamp forward in time to make sure to get the last entry from this batch in the next query
				end = lastEntry[0].Timestamp.Add(1 * time.Nanosecond)
			}
		}
	}

	if partFile != nil {
		if err := partFile.Finalize(); err != nil {
			log.Fatalln(err)
		}
	}
}

func (q *Query) outputFilename() string {
	return fmt.Sprintf(
		"%s_%s_%s.part",
		q.PartPathPrefix,
		q.Start.UTC().Format("20060102T150405"),
		q.End.UTC().Format("20060102T150405"),
	)
}

// createPartFile returns a PartFile.
// The bool value shows if the part file already exists, and this range should be skipped.
func (q *Query) createPartFile() (*PartFile, bool) {
	partFile := NewPartFile(q.outputFilename())

	if !q.OverwriteCompleted {
		// If we already have the completed file, no need to download it again.
		// The user can delete the files if they want to download parts again.
		exists, err := partFile.Exists()
		if err != nil {
			log.Fatalf("Query failed: %s\n", err)
		}
		if exists {
			log.Printf("Skip range: %s - %s: already downloaded\n", q.Start, q.End)
			return nil, true
		}
	}

	if err := partFile.CreateTempFile(); err != nil {
		log.Fatalf("Query failed: %s\n", err)
	}

	return partFile, false
}

// rounds up duration d by the multiple m, and then divides by m.
func ceilingDivision(d, m time.Duration) int64 {
	return int64((d + m - 1) / m)
}

// Returns the next job's start and end times.
func (q *Query) nextJob(start, end time.Time) (time.Time, time.Time) {
	if q.Forward {
		start = end
		return start, minTime(start.Add(q.ParallelDuration), q.End)
	}

	end = start
	return maxTime(end.Add(-q.ParallelDuration), q.Start), end
}

type parallelJob struct {
	q    *Query
	done chan struct{}
}

func newParallelJob(q *Query) *parallelJob {
	return &parallelJob{
		q:    q,
		done: make(chan struct{}, 1),
	}
}

func (j *parallelJob) run(c client.Client, out output.LogOutput, statistics bool) {
	j.q.DoQuery(c, out, statistics)
	j.done <- struct{}{}
}

func (q *Query) parallelJobs() []*parallelJob {
	nJobs := ceilingDivision(q.End.Sub(q.Start), q.ParallelDuration)
	jobs := make([]*parallelJob, nJobs)

	// Normally `nextJob` will swap the start/end to get the next job. Here, we swap them
	// on input so that we calculate the starting job instead of the next job.
	start, end := q.nextJob(q.End, q.Start)

	// Queue up jobs
	for i := range jobs {
		rq := *q
		rq.Start = start
		rq.End = end

		jobs[i] = newParallelJob(&rq)

		start, end = q.nextJob(start, end)
	}

	return jobs
}

// Waits for each job to finish in order, reads the part file and copies it to stdout
func (q *Query) mergeJobs(jobs []*parallelJob) error {
	if !q.MergeParts {
		return nil
	}

	for _, job := range jobs {
		// wait for the next job to finish
		<-job.done

		f, err := os.Open(job.q.outputFilename())
		if err != nil {
			return fmt.Errorf("open file error: %w", err)
		}
		defer f.Close()

		_, err = io.Copy(os.Stdout, f)
		if err != nil {
			return fmt.Errorf("copying file error: %w", err)
		}

		if !q.KeepParts {
			err := os.Remove(job.q.outputFilename())
			if err != nil {
				return fmt.Errorf("removing file error: %w", err)
			}
		}
	}

	return nil
}

// Starts `ParallelMaxWorkers` number of workers to process all of the `parallelJob`s
// This function is non-blocking. The caller should `Wait` on the returned `WaitGroup`.
func (q *Query) startWorkers(
	jobs []*parallelJob,
	c client.Client,
	out output.LogOutput,
	statistics bool,
) *sync.WaitGroup {
	wg := sync.WaitGroup{}
	jobsChan := make(chan *parallelJob, len(jobs))

	// Queue up the jobs
	// There is a possible optimization here to use an unbuffered channel,
	// But the memory and CPU overhead for yet another go routine makes me
	// think that this optimization is not worth it. So I used a buffered
	// channel instead.
	for _, job := range jobs {
		jobsChan <- job
	}
	close(jobsChan)

	// Start workers
	for w := 0; w < q.ParallelMaxWorkers; w++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			for job := range jobsChan {
				job.run(c, out, statistics)
			}
		}()
	}

	return &wg
}

func (q *Query) DoQueryParallel(c client.Client, out output.LogOutput, statistics bool) {
	if q.ParallelDuration < 1 {
		log.Fatalf("Parallel duration has to be a positive value\n")
	}

	jobs := q.parallelJobs()

	wg := q.startWorkers(jobs, c, out, statistics)

	if err := q.mergeJobs(jobs); err != nil {
		log.Fatalf("Merging part files error: %s\n", err)
	}

	wg.Wait()
}

func minTime(t1, t2 time.Time) time.Time {
	if t1.Before(t2) {
		return t1
	}
	return t2
}

func maxTime(t1, t2 time.Time) time.Time {
	if t1.After(t2) {
		return t1
	}
	return t2
}

func getLatestConfig(client chunk.ObjectClient, orgID string) (*config.SchemaConfig, error) {
	// Get the latest
	iteration := 0
	searchFor := fmt.Sprintf("%s-%s.yaml", orgID, schemaConfigFilename) // schemaconfig-tenant.yaml
	var loadedSchema *config.SchemaConfig
	for {
		if iteration != 0 {
			searchFor = fmt.Sprintf("%s-%s-%d.yaml", orgID, schemaConfigFilename, iteration) // tenant-schemaconfig-1.yaml
		}
		tempSchema, err := LoadSchemaUsingObjectClient(client, searchFor)
		if err == errNotExists {
			break
		}
		if err != nil {
			return nil, err
		}

		loadedSchema = tempSchema
		iteration++
	}
	if loadedSchema != nil {
		return loadedSchema, nil
	}

	searchFor = fmt.Sprintf("%s.yaml", schemaConfigFilename) // schemaconfig.yaml for backwards compatibility
	loadedSchema, err := LoadSchemaUsingObjectClient(client, searchFor)
	if err == nil {
		return loadedSchema, nil
	}
	if err != errNotExists {
		return nil, err
	}
	return nil, errors.Wrap(err, "could not find a schema config file matching any of the known patterns. First verify --org-id is correct. Then check the root of the bucket for a file with `schemaconfig` in the name. If no such file exists it may need to be created or re-synced from the source.")
}

// DoLocalQuery executes the query against the local store using a Loki configuration file.
func (q *Query) DoLocalQuery(out output.LogOutput, statistics bool, orgID string, useRemoteSchema bool) error {
	var conf loki.Config
	conf.RegisterFlags(flag.CommandLine)
	if q.LocalConfig == "" {
		return errors.New("no supplied config file")
	}
	if err := cfg.YAML(q.LocalConfig, false, true)(&conf); err != nil {
		return err
	}

	cm := storage.NewClientMetrics()
	if useRemoteSchema {
		if q.SchemaStore == "" {
			return fmt.Errorf("failed to fetch remote schema. -schema-store is not set")
		}

		client, err := GetObjectClient(q.SchemaStore, conf, cm)
		if err != nil {
			return err
		}

		loadedSchema, err := getLatestConfig(client, orgID)
		if err != nil {
			return err
		}
		conf.SchemaConfig = *loadedSchema
	}

	if err := conf.Validate(); err != nil {
		return err
	}

	limits, err := validation.NewOverrides(conf.LimitsConfig, nil)
	if err != nil {
		return err
	}
	conf.StorageConfig.BoltDBShipperConfig.Mode = indexshipper.ModeReadOnly
	conf.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled = true
	conf.StorageConfig.TSDBShipperConfig.Mode = indexshipper.ModeReadOnly
	conf.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled = true

	querier, err := storage.NewStore(conf.StorageConfig, conf.ChunkStoreConfig, conf.SchemaConfig, limits, cm, prometheus.DefaultRegisterer, util_log.Logger, constants.Loki)
	if err != nil {
		return err
	}

	eng := logql.NewEngine(conf.Querier.Engine, querier, limits, util_log.Logger)
	var query logql.Query

	if q.isInstant() {
		params, err := logql.NewLiteralParams(
			q.QueryString,
			q.Start,
			q.Start,
			0,
			0,
			q.resultsDirection(),
			uint32(q.Limit),
			nil,
			nil,
		)
		if err != nil {
			return err
		}

		query = eng.Query(params)
	} else {
		params, err := logql.NewLiteralParams(
			q.QueryString,
			q.Start,
			q.End,
			q.Step,
			q.Interval,
			q.resultsDirection(),
			uint32(q.Limit),
			nil,
			nil,
		)
		if err != nil {
			return err
		}

		query = eng.Query(params)
	}

	// execute the query
	ctx := user.InjectOrgID(context.Background(), orgID)
	result, err := query.Exec(ctx)
	if err != nil {
		return err
	}

	resPrinter := print.NewQueryResultPrinter(q.ShowLabelsKey, q.IgnoreLabelsKey, q.Quiet, q.FixedLabelsLen, q.Forward, q.IncludeCommonLabels)
	if statistics {
		resPrinter.PrintStats(result.Statistics)
	}

	value, err := marshal.NewResultValue(result.Data)
	if err != nil {
		return err
	}

	resPrinter.PrintResult(value, out, nil)
	return nil
}

func GetObjectClient(store string, conf loki.Config, cm storage.ClientMetrics) (chunk.ObjectClient, error) {
	return storage.NewObjectClient(store, "logcli-query", conf.StorageConfig, cm)
}

var errNotExists = stdErrors.New("doesn't exist")

type schemaConfigSection struct {
	config.SchemaConfig `yaml:"schema_config"`
}

// LoadSchemaUsingObjectClient returns the loaded schema from the object with the given name
func LoadSchemaUsingObjectClient(oc chunk.ObjectClient, name string) (*config.SchemaConfig, error) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Minute))
	defer cancel()

	ok, err := oc.ObjectExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNotExists
	}

	rdr, _, err := oc.GetObject(ctx, name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load schema object '%s'", name)
	}
	defer rdr.Close()

	decoder := yaml.NewDecoder(rdr)
	decoder.SetStrict(true)
	section := schemaConfigSection{}
	err = decoder.Decode(&section)
	if err != nil {
		return nil, err
	}

	return &section.SchemaConfig, nil
}

// SetInstant makes the Query an instant type
func (q *Query) SetInstant(time time.Time) {
	q.Start = time
	q.End = time
}

func (q *Query) isInstant() bool {
	return q.Start == q.End && q.Step == 0
}

func (q *Query) resultsDirection() logproto.Direction {
	if q.Forward {
		return logproto.FORWARD
	}
	return logproto.BACKWARD
}
