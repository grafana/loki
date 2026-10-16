package query

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/lokitool/client"
	"github.com/grafana/loki/v3/pkg/lokitool/output"
	"github.com/grafana/loki/v3/pkg/lokitool/print"
)

// Query contains all necessary fields to execute instant and range queries and print the results.
type Query struct {
	QueryString         string
	Start               time.Time
	End                 time.Time
	Limit               int
	BatchSize           int
	Forward             bool
	Step                time.Duration
	Interval            time.Duration
	NoLabels            bool
	IgnoreLabelsKey     []string
	ShowLabelsKey       []string
	IncludeCommonLabels bool
	FixedLabelsLen      int
	ColoredOutput       bool

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
func (q *Query) DoQuery(c client.QueryClient, out output.LogOutput, statistics bool) error {
	d := q.resultsDirection()

	var resp *loghttp.QueryResponse
	var err error

	var partFile *PartFile
	if q.PartPathPrefix != "" {
		var shouldSkip bool
		partFile, shouldSkip, err = q.createPartFile()
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}

		// createPartFile will return true if the part file exists and
		// OverwriteCompleted is false, therefor, we should exit the function
		// here because we have nothing to do.
		if shouldSkip {
			return nil
		}
	}

	if partFile != nil {
		defer partFile.Close()
		out = out.WithWriter(partFile)
	}

	result := print.NewQueryResultPrinter(q.ShowLabelsKey, q.IgnoreLabelsKey, q.FixedLabelsLen, q.Forward, q.IncludeCommonLabels)

	if q.isInstant() {
		resp, err = c.Query(q.QueryString, q.Limit, q.Start, d)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		if statistics {
			result.PrintStats(resp.Data.Statistics)
		}
		if _, _, err := result.PrintResult(resp.Data.Result, out, nil); err != nil {
			return err
		}
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
			resp, err = c.QueryRange(q.QueryString, bs, start, end, d, q.Step, q.Interval)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if statistics {
				result.PrintStats(resp.Data.Statistics)
			}

			var prErr error
			resultLength, lastEntry, prErr = result.PrintResult(resp.Data.Result, out, lastEntry)
			if prErr != nil {
				return prErr
			}
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
				return fmt.Errorf("invalid batch size %v, the next query will have %v overlapping entries "+
					"(there will always be 1 overlapping entry but Loki allows multiple entries to have "+
					"the same timestamp, so when a batch ends in this scenario the next query will include "+
					"all the overlapping entries again).  Please increase your batch size to at least %v to account "+
					"for overlapping entries", q.BatchSize, len(lastEntry), len(lastEntry)+1)
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
			return err
		}
	}
	return nil
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
func (q *Query) createPartFile() (*PartFile, bool, error) {
	partFile := NewPartFile(q.outputFilename())

	if !q.OverwriteCompleted {
		// If we already have the completed file, no need to download it again.
		// The user can delete the files if they want to download parts again.
		exists, err := partFile.Exists()
		if err != nil {
			return nil, false, fmt.Errorf("checking part file existence: %w", err)
		}
		if exists {
			log.Debugf("Skip range: %s - %s: already downloaded", q.Start, q.End)
			return nil, true, nil
		}
	}

	if err := partFile.CreateTempFile(); err != nil {
		return nil, false, fmt.Errorf("creating part file: %w", err)
	}

	return partFile, false, nil
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
	err  error
}

func newParallelJob(q *Query) *parallelJob {
	return &parallelJob{
		q:    q,
		done: make(chan struct{}, 1),
	}
}

func (j *parallelJob) run(c client.QueryClient, out output.LogOutput, statistics bool) {
	j.err = j.q.DoQuery(c, out, statistics)
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
	c client.QueryClient,
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

func (q *Query) DoQueryParallel(c client.QueryClient, out output.LogOutput, statistics bool) error {
	if q.ParallelDuration < 1 {
		return fmt.Errorf("parallel duration has to be a positive value")
	}

	jobs := q.parallelJobs()

	wg := q.startWorkers(jobs, c, out, statistics)

	if err := q.mergeJobs(jobs); err != nil {
		return fmt.Errorf("merging part files error: %w", err)
	}

	wg.Wait()

	for _, job := range jobs {
		if job.err != nil {
			return job.err
		}
	}
	return nil
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
