package executor

// inputMeterBatchRows is how many rows an inputMeter counts before it reports
// their bytes.
const inputMeterBatchRows = 1000

// inputMeter counts the bytes of the rows that one merge task reads.
//
// It reports the bytes in batches while the task runs, not once at the end.
// So rate() of the counter behind report stays steady for a task that runs
// for many minutes, and the per-row path does not call report.
type inputMeter struct {
	report func(bytes int64)

	reported  int64
	batch     int64
	batchRows int
}

// newInputMeter returns an inputMeter that reports each batch to report.
func newInputMeter(report func(bytes int64)) *inputMeter {
	return &inputMeter{report: report}
}

// discardInputMeter returns an inputMeter that reports to nothing.
func discardInputMeter() *inputMeter {
	return newInputMeter(func(int64) {})
}

// add counts one row of the given size in bytes.
func (m *inputMeter) add(bytes int64) {
	m.batch += bytes
	m.batchRows++
	if m.batchRows >= inputMeterBatchRows {
		m.flush()
	}
}

// flush reports the bytes that add counted after the last report.
func (m *inputMeter) flush() {
	if m.batchRows == 0 {
		return
	}
	m.report(m.batch)
	m.reported += m.batch
	m.batch, m.batchRows = 0, 0
}

// total returns every byte that add counted, reported or not.
func (m *inputMeter) total() int64 {
	return m.reported + m.batch
}
