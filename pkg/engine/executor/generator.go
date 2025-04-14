package executor

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// Lorem ipsum words to choose from
var words = []string{
	"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit",
	"sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore", "et", "dolore",
	"magna", "aliqua", "enim", "ad", "minim", "veniam", "quis", "nostrud", "exercitation",
	"ullamco", "laboris", "nisi", "ut", "aliquip", "ex", "ea", "commodo", "consequat",
	"duis", "aute", "irure", "dolor", "in", "reprehenderit", "voluptate", "velit",
	"esse", "cillum", "dolore", "eu", "fugiat", "nulla", "pariatur", "excepteur",
	"sint", "occaecat", "cupidatat", "non", "proident", "sunt", "in", "culpa", "qui",
	"officia", "deserunt", "mollit", "anim", "id", "est", "laborum",
}

func randomWords(i int) string {
	result := make([]string, i)
	for j := 0; j < i; j++ {
		// Pick a random word from the list
		idx := rand.IntN(len(words))
		result[j] = words[idx]
	}

	return strings.Join(result, " ")
}

type dataGenerator struct {
	limit int64
}

// Accept implements physical.Node.
func (d *dataGenerator) Accept(physical.Visitor) error {
	return nil
}

// ID implements physical.Node.
func (d *dataGenerator) ID() string {
	return fmt.Sprintf("%p", d)
}

// Type implements physical.Node.
func (d *dataGenerator) Type() physical.NodeType {
	return math.MaxUint32
}

var _ physical.Node = (*dataGenerator)(nil)

func createBatch(idx int64, n int64) arrow.Record {
	// 1. Create a memory allocator
	mem := memory.NewGoAllocator()

	// 2. Define the schema
	schema := arrow.NewSchema(
		[]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "log", Type: arrow.BinaryTypes.String},
			{Name: "timestamp", Type: arrow.PrimitiveTypes.Uint64},
		},
		nil, // No metadata
	)

	// 3. Create builders for each column
	idBuilder := array.NewInt64Builder(mem)
	defer idBuilder.Release()

	logBuilder := array.NewStringBuilder(mem)
	defer logBuilder.Release()

	timestampBuilder := array.NewUint64Builder(mem)
	defer timestampBuilder.Release()

	// 4. Append data to the builders
	ids := make([]int64, n)
	logs := make([]string, n)
	ts := make([]uint64, n)

	for i := range n {
		ids[i] = idx + int64(i)
		logs[i] = randomWords(10)
		ts[i] = uint64(time.Now().UnixNano())
	}

	timestampBuilder.AppendValues(ts, nil)
	idBuilder.AppendValues(ids, nil)
	logBuilder.AppendValues(logs, nil)

	// 5. Build the arrays
	idArray := idBuilder.NewArray()
	defer idArray.Release()

	nameArray := logBuilder.NewArray()
	defer nameArray.Release()

	valueArray := timestampBuilder.NewArray()
	defer valueArray.Release()

	// 6. Create the record
	columns := []arrow.Array{idArray, nameArray, valueArray}
	record := array.NewRecord(schema, columns, n)

	return record
}
