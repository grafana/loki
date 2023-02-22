package query

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	json "github.com/json-iterator/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/user"
	"gopkg.in/yaml.v2"

	"github.com/grafana/dskit/multierror"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	chunk "github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/validation"
)

const schemaConfigFilename = "schemaconfig"

type streamEntryPair struct {
	entry  loghttp.Entry
	labels loghttp.LabelSet
}

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
	FixedLabelsLen         int
	ColoredOutput          bool
	LocalConfig            string
	FetchSchemaFromStorage bool

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

	if q.isInstant() {
		resp, err = c.Query(q.QueryString, q.Limit, q.Start, d, q.Quiet)
		if err != nil {
			log.Fatalf("Query failed: %+v", err)
		}
		if statistics {
			q.printStats(resp.Data.Statistics)
		}
		_, _ = q.printResult(resp.Data.Result, out, nil)
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
				q.printStats(resp.Data.Statistics)
			}

			resultLength, lastEntry = q.printResult(resp.Data.Result, out, lastEntry)
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

func (q *Query) printResult(value loghttp.ResultValue, out output.LogOutput, lastEntry []*loghttp.Entry) (int, []*loghttp.Entry) {
	length := -1
	var entry []*loghttp.Entry
	switch value.Type() {
	case logqlmodel.ValueTypeStreams:
		length, entry = q.printStream(value.(loghttp.Streams), out, lastEntry)
	case loghttp.ResultTypeScalar:
		q.printScalar(value.(loghttp.Scalar))
	case loghttp.ResultTypeMatrix:
		q.printMatrix(value.(loghttp.Matrix))
	case loghttp.ResultTypeVector:
		q.printVector(value.(loghttp.Vector))
	default:
		log.Fatalf("Unable to print unsupported type: %v", value.Type())
	}
	return length, entry
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
		client, err := GetObjectClient(conf, cm)
		if err != nil {
			return err
		}

		objects := []string{
			fmt.Sprintf("%s-%s.yaml", orgID, schemaConfigFilename), // schemaconfig-tenant.yaml
			fmt.Sprintf("%s.yaml", schemaConfigFilename),           // schemaconfig.yaml for backwards compatibility
		}
		loadedSchema, err := LoadSchemaUsingObjectClient(client, objects...)
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

	querier, err := storage.NewStore(conf.StorageConfig, conf.ChunkStoreConfig, conf.SchemaConfig, limits, cm, prometheus.DefaultRegisterer, util_log.Logger)
	if err != nil {
		return err
	}

	eng := logql.NewEngine(conf.Querier.Engine, querier, limits, util_log.Logger)
	var query logql.Query

	if q.isInstant() {
		query = eng.Query(logql.NewLiteralParams(
			q.QueryString,
			q.Start,
			q.Start,
			0,
			0,
			q.resultsDirection(),
			uint32(q.Limit),
			nil,
		))
	} else {
		query = eng.Query(logql.NewLiteralParams(
			q.QueryString,
			q.Start,
			q.End,
			q.Step,
			q.Interval,
			q.resultsDirection(),
			uint32(q.Limit),
			nil,
		))
	}

	// execute the query
	ctx := user.InjectOrgID(context.Background(), orgID)
	result, err := query.Exec(ctx)
	if err != nil {
		return err
	}

	if statistics {
		q.printStats(result.Statistics)
	}

	value, err := marshal.NewResultValue(result.Data)
	if err != nil {
		return err
	}

	q.printResult(value, out, nil)
	return nil
}

func GetObjectClient(conf loki.Config, cm storage.ClientMetrics) (chunk.ObjectClient, error) {
	oc, err := storage.NewObjectClient(
		conf.StorageConfig.BoltDBShipperConfig.SharedStoreType,
		conf.StorageConfig,
		cm,
	)
	if err != nil {
		return nil, err
	}
	return oc, nil
}

type schemaConfigSection struct {
	config.SchemaConfig `yaml:"schema_config"`
}

// LoadSchemaUsingObjectClient returns the loaded schema from the first found object
func LoadSchemaUsingObjectClient(oc chunk.ObjectClient, names ...string) (*config.SchemaConfig, error) {
	errors := multierror.New()
	for _, name := range names {
		schema, err := func(name string) (*config.SchemaConfig, error) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
			defer cancel()
			rdr, _, err := oc.GetObject(ctx, name)
			if err != nil {
				return nil, err
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
		}(name)

		if err != nil {
			errors = append(errors, err)
			continue
		}
		return schema, nil
	}
	return nil, errors.Err()
}

// SetInstant makes the Query an instant type
func (q *Query) SetInstant(time time.Time) {
	q.Start = time
	q.End = time
}

func (q *Query) isInstant() bool {
	return q.Start == q.End && q.Step == 0
}

func (q *Query) printStream(streams loghttp.Streams, out output.LogOutput, lastEntry []*loghttp.Entry) (int, []*loghttp.Entry) {
	common := commonLabels(streams)

	// Remove the labels we want to show from common
	if len(q.ShowLabelsKey) > 0 {
		common = matchLabels(false, common, q.ShowLabelsKey)
	}

	if len(common) > 0 && !q.Quiet {
		log.Println("Common labels:", color.RedString(common.String()))
	}

	if len(q.IgnoreLabelsKey) > 0 && !q.Quiet {
		log.Println("Ignoring labels key:", color.RedString(strings.Join(q.IgnoreLabelsKey, ",")))
	}

	if len(q.ShowLabelsKey) > 0 && !q.Quiet {
		log.Println("Print only labels key:", color.RedString(strings.Join(q.ShowLabelsKey, ",")))
	}

	// Remove ignored and common labels from the cached labels and
	// calculate the max labels length
	maxLabelsLen := q.FixedLabelsLen
	for i, s := range streams {
		// Remove common labels
		ls := subtract(s.Labels, common)

		if len(q.ShowLabelsKey) > 0 {
			ls = matchLabels(true, ls, q.ShowLabelsKey)
		}

		// Remove ignored labels
		if len(q.IgnoreLabelsKey) > 0 {
			ls = matchLabels(false, ls, q.IgnoreLabelsKey)
		}

		// Overwrite existing Labels
		streams[i].Labels = ls

		// Update max labels length
		len := len(ls.String())
		if maxLabelsLen < len {
			maxLabelsLen = len
		}
	}

	// sort and display entries
	allEntries := make([]streamEntryPair, 0)

	for _, s := range streams {
		for _, e := range s.Entries {
			allEntries = append(allEntries, streamEntryPair{
				entry:  e,
				labels: s.Labels,
			})
		}
	}

	if len(allEntries) == 0 {
		return 0, nil
	}

	if q.Forward {
		sort.Slice(allEntries, func(i, j int) bool { return allEntries[i].entry.Timestamp.Before(allEntries[j].entry.Timestamp) })
	} else {
		sort.Slice(allEntries, func(i, j int) bool { return allEntries[i].entry.Timestamp.After(allEntries[j].entry.Timestamp) })
	}

	printed := 0
	for _, e := range allEntries {
		// Skip the last entry if it overlaps, this happens because batching includes the last entry from the last batch
		if len(lastEntry) > 0 && e.entry.Timestamp == lastEntry[0].Timestamp {
			skip := false
			// Because many logs can share a timestamp in the unlucky event a batch ends with a timestamp
			// shared by multiple entries we have to check all that were stored to see if we've already
			// printed them.
			for _, le := range lastEntry {
				if e.entry.Line == le.Line {
					skip = true
				}
			}
			if skip {
				continue
			}
		}
		out.FormatAndPrintln(e.entry.Timestamp, e.labels, maxLabelsLen, e.entry.Line)
		printed++
	}

	// Loki allows multiple entries at the same timestamp, this is a bit of a mess if a batch ends
	// with an entry that shared multiple timestamps, so we need to keep a list of all these entries
	// because the next query is going to contain them too and we want to not duplicate anything already
	// printed.
	lel := []*loghttp.Entry{}
	// Start with the timestamp of the last entry
	le := allEntries[len(allEntries)-1].entry
	for i, e := range allEntries {
		// Save any entry which has this timestamp (most of the time this will only be the single last entry)
		if e.entry.Timestamp.Equal(le.Timestamp) {
			lel = append(lel, &allEntries[i].entry)
		}
	}

	return printed, lel
}

func (q *Query) printMatrix(matrix loghttp.Matrix) {
	// yes we are effectively unmarshalling and then immediately marshalling this object back to json.  we are doing this b/c
	// it gives us more flexibility with regard to output types in the future.  initially we are supporting just formatted json but eventually
	// we might add output options such as render to an image file on disk
	bytes, err := json.MarshalIndent(matrix, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling matrix: %v", err)
	}

	fmt.Print(string(bytes))
}

func (q *Query) printVector(vector loghttp.Vector) {
	bytes, err := json.MarshalIndent(vector, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling vector: %v", err)
	}

	fmt.Print(string(bytes))
}

func (q *Query) printScalar(scalar loghttp.Scalar) {
	bytes, err := json.MarshalIndent(scalar, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling scalar: %v", err)
	}

	fmt.Print(string(bytes))
}

type kvLogger struct {
	*tabwriter.Writer
}

func (k kvLogger) Log(keyvals ...interface{}) error {
	for i := 0; i < len(keyvals); i += 2 {
		fmt.Fprintln(k.Writer, color.BlueString("%s", keyvals[i]), "\t", fmt.Sprintf("%v", keyvals[i+1]))
	}
	k.Flush()
	return nil
}

func (q *Query) printStats(stats stats.Result) {
	writer := tabwriter.NewWriter(os.Stderr, 0, 8, 0, '\t', 0)
	stats.Log(kvLogger{Writer: writer})
}

func (q *Query) resultsDirection() logproto.Direction {
	if q.Forward {
		return logproto.FORWARD
	}
	return logproto.BACKWARD
}
