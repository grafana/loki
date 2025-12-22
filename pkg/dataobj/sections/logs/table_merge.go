package logs

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"runtime"
	"sync"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// A pool for valueBatch objects, with a pre-allocated row buffer.
var valueBatchPool = &sync.Pool{
	New: func() any {
		return &valueBatch{
			rows: make([]rowValue, 1024),
		}
	},
}

// mergeTablesIncremental incrementally merges the provides sorted tables into
// a single table. Incremental merging limits memory overhead as only mergeSize
// tables are open at a time.
//
// mergeTablesIncremental panics if maxMergeSize is less than 2.
func mergeTablesIncremental(buf *tableBuffer, pageSize, pageRowCount int, compressionOpts *dataset.CompressionOptions, tables []*table, maxMergeSize int, sort SortOrder) (*table, error) {
	if maxMergeSize < 2 {
		panic("mergeTablesIncremental: merge size must be at least 2, got " + fmt.Sprint(maxMergeSize))
	}

	// Even if there's only one table, we still pass to mergeTables to ensure
	// it's compressed with compressionOpts.
	if len(tables) == 1 {
		return mergeTables(buf, pageSize, pageRowCount, compressionOpts, tables, sort)
	}

	in := tables

	for len(in) > 1 {
		var out []*table

		for i := 0; i < len(in); i += maxMergeSize {
			set := in[i:min(i+maxMergeSize, len(in))]
			merged, err := mergeTables(buf, pageSize, pageRowCount, compressionOpts, set, sort)
			if err != nil {
				return nil, err
			}
			out = append(out, merged)
		}

		in = out
	}

	return in[0], nil
}

type rowValue struct {
	builder *dataset.ColumnBuilder
	row     int
	value   dataset.Value
}

// mergeTables merges the provided sorted tables into a new single sorted table
// using k-way merge.
func mergeTables(buf *tableBuffer, pageSize, pageRowCount int, compressionOpts *dataset.CompressionOptions, tables []*table, sort SortOrder) (*table, error) {
	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize, pageRowCount)
		timestampBuilder = buf.Timestamp(pageSize, pageRowCount)
		messageBuilder   = buf.Message(pageSize, pageRowCount, compressionOpts)
	)

	tableSequences := make([]*tableSequence, 0, len(tables))
	for _, t := range tables {
		dsetColumns, err := result.Collect(t.ListColumns(context.Background()))
		if err != nil {
			return nil, err
		}

		r := dataset.NewReader(dataset.ReaderOptions{
			Dataset: t,
			Columns: dsetColumns,

			// The table is in memory, so don't prefetch.
			Prefetch: false,
		})

		tableSequences = append(tableSequences, &tableSequence{
			columns:         dsetColumns,
			DatasetSequence: NewDatasetSequence(r, 128),
		})
	}

	maxValue := result.Value(dataset.Row{
		Index: math.MaxInt,
		Values: []dataset.Value{
			dataset.Int64Value(math.MaxInt64), // StreamID
			dataset.Int64Value(math.MinInt64), // Timestamp
		},
	})

	var rows int

	// Start async workers in order to improve throughput
	wg := &sync.WaitGroup{}
	appenderCount := max(2, runtime.GOMAXPROCS(0))
	appenders := make([]*columnAppender, 0, appenderCount)
	for range appenderCount {
		appenders = append(appenders, newColumnAppender(wg))
	}

	tree := loser.New(tableSequences, maxValue, tableSequenceAt, CompareForSortOrder(sort), tableSequenceClose)
	defer tree.Close()

	var prev dataset.Row
	for tree.Next() {
		seq := tree.Winner()

		row, err := tableSequenceAt(seq).Value()
		if err != nil {
			return nil, err
		}

		if equalRows(prev, row) {
			// Skip equal rows
			continue
		}
		prev = row

		for i, column := range seq.columns {
			// column is guaranteed to be a *tableColumn since we got it from *table.
			column := column.(*tableColumn)

			// dataset.Iter returns values in the same order as the number of
			// columns.
			value := row.Values[i]

			switch column.Type {
			case ColumnTypeStreamID:
				appenders[0].appendValue(streamIDBuilder, rows, value)
			case ColumnTypeTimestamp:
				appenders[0].appendValue(timestampBuilder, rows, value)
			case ColumnTypeMetadata:
				index, columnBuilder := buf.Metadata(column.Desc.Tag, pageSize, pageRowCount, compressionOpts)
				appenders[index%len(appenders)].appendValue(columnBuilder, rows, value)
			case ColumnTypeMessage:
				// The message, stream ID and timestamp columns are written on every row. We choose to send message to a separate appender to ensure basic work is also distributed.
				appenders[1].appendValue(messageBuilder, rows, value)
			default:
				return nil, fmt.Errorf("unknown column type %s", column.Type)
			}
		}

		rows++
	}

	for _, appender := range appenders {
		appender.close()
	}
	wg.Wait()

	return buf.Flush()
}

// valueBatch references a batch of rowValues.
type valueBatch struct {
	rows []rowValue
	size int
}

// columnAppender writes column values to columns.
// They are thread-safe via the appendValue method and will apply values in the order they are received.
// When using multiple appenders, each distinct column should only be written to a single appender in order to maintain ordering.
type columnAppender struct {
	waitGroup *sync.WaitGroup
	workChan  chan *valueBatch
	batch     *valueBatch
}

func newColumnAppender(wg *sync.WaitGroup) *columnAppender {
	appender := &columnAppender{
		workChan:  make(chan *valueBatch),
		waitGroup: wg,
		batch:     valueBatchPool.Get().(*valueBatch),
	}
	go appender.loop()

	return appender
}

func (ca *columnAppender) loop() {
	ca.waitGroup.Add(1)
	for batch := range ca.workChan {
		for _, rowValue := range batch.rows[:batch.size] {
			err := rowValue.builder.Append(rowValue.row, rowValue.value)
			if err != nil {
				panic(err)
			}
		}

		batch.size = 0
		valueBatchPool.Put(batch)
	}
	ca.waitGroup.Done()
}

// appendValue adds a value to the current batch. If the batch is full, it is sent to the worker and a new batch is started.
func (ca *columnAppender) appendValue(builder *dataset.ColumnBuilder, row int, value dataset.Value) {
	ca.batch.rows[ca.batch.size].builder = builder
	ca.batch.rows[ca.batch.size].row = row
	ca.batch.rows[ca.batch.size].value = value
	ca.batch.size++

	if ca.batch.size >= len(ca.batch.rows) {
		ca.workChan <- ca.batch
		ca.batch = valueBatchPool.Get().(*valueBatch)
	}
}

func (ca *columnAppender) close() {
	ca.workChan <- ca.batch
	close(ca.workChan)
}

type tableSequence struct {
	DatasetSequence
	columns []dataset.Column
}

var _ loser.Sequence = (*tableSequence)(nil)

func tableSequenceAt(seq *tableSequence) result.Result[dataset.Row] { return seq.At() }
func tableSequenceClose(seq *tableSequence)                         { seq.Close() }

func NewDatasetSequence(r *dataset.Reader, bufferSize int) DatasetSequence {
	return DatasetSequence{
		r:          r,
		bufferSize: bufferSize,
	}
}

type DatasetSequence struct {
	bufferSize int
	curValue   result.Result[dataset.Row]

	r *dataset.Reader

	buf  []dataset.Row
	off  int // Offset into buf
	size int // Number of valid values in buf
}

func (seq *DatasetSequence) Next() bool {
	if seq.off < seq.size {
		seq.curValue = result.Value(seq.buf[seq.off])
		seq.off++
		return true
	}

	seq.buf = make([]dataset.Row, seq.bufferSize)
ReadBatch:
	n, err := seq.r.Read(context.Background(), seq.buf)
	if err != nil && !errors.Is(err, io.EOF) {
		seq.curValue = result.Error[dataset.Row](err)
		return true
	} else if n == 0 && errors.Is(err, io.EOF) {
		return false
	} else if n == 0 {
		// Re-read if we got an empty batch without hitting EOF.
		goto ReadBatch
	}

	seq.curValue = result.Value(seq.buf[0])

	seq.off = 1
	seq.size = n
	return true
}

func (seq *DatasetSequence) At() result.Result[dataset.Row] {
	return seq.curValue
}

func (seq *DatasetSequence) Close() {
	_ = seq.r.Close()
}

// CompareForSortOrder returns a comparison function for result rows for the given sort order.
func CompareForSortOrder(sort SortOrder) func(result.Result[dataset.Row], result.Result[dataset.Row]) bool {
	switch sort {
	case SortStreamASC:
		return func(a, b result.Result[dataset.Row]) bool {
			return result.Compare(a, b, compareRowsStreamID) < 0
		}
	case SortTimestampDESC:
		return func(a, b result.Result[dataset.Row]) bool {
			return result.Compare(a, b, compareRowsTimestamp) < 0
		}
	default:
		panic("invalid sort order")
	}
}

// compareRowsStreamID compares two dataset rows based on sort order [streamID ASC, timestamp DESC].
func compareRowsStreamID(a, b dataset.Row) int {
	aStreamID, bStreamID, aTimestamp, bTimestamp := valuesForRows(a, b)
	if res := cmp.Compare(aStreamID, bStreamID); res != 0 {
		return res
	}
	return cmp.Compare(bTimestamp, aTimestamp)
}

// compareRowsStreamID compares two dataset rows based on sort order [timestamp DESC, streamID ASC].
func compareRowsTimestamp(a, b dataset.Row) int {
	aStreamID, bStreamID, aTimestamp, bTimestamp := valuesForRows(a, b)
	if res := cmp.Compare(bTimestamp, aTimestamp); res != 0 {
		return res
	}
	return cmp.Compare(aStreamID, bStreamID)
}

// valuesForRows returns the streamID and timestamp values from rows a and b.
func valuesForRows(a, b dataset.Row) (aStreamID int64, bStreamID int64, aTimestamp int64, bTimestamp int64) {
	aStreamID = a.Values[0].Int64()
	bStreamID = b.Values[0].Int64()
	aTimestamp = a.Values[1].Int64()
	bTimestamp = b.Values[1].Int64()
	return
}

// equalRows compares two rows for equality, column by column.
// a row is considered equal if all the columns are equal.
func equalRows(a, b dataset.Row) bool {
	if len(a.Values) != len(b.Values) {
		return false
	}

	// The first two columns of each row are *always* stream ID and timestamp, so they will be checked first.
	// This means equalRows will exit quickly for rows with different timestamps without reading the rest of the columns.
	for i := 0; i < len(a.Values); i++ {
		aType, bType := a.Values[i].Type(), b.Values[i].Type()
		if aType != bType {
			return false
		}

		switch aType {
		case datasetmd.PHYSICAL_TYPE_INT64:
			if a.Values[i].Int64() != b.Values[i].Int64() {
				return false
			}
		case datasetmd.PHYSICAL_TYPE_UINT64:
			if a.Values[i].Uint64() != b.Values[i].Uint64() {
				return false
			}
		case datasetmd.PHYSICAL_TYPE_BINARY:
			if !bytes.Equal(a.Values[i].Binary(), b.Values[i].Binary()) {
				return false
			}
		case datasetmd.PHYSICAL_TYPE_UNSPECIFIED:
			continue
		default:
			return false
		}
	}

	return true
}
