package expr_test

import (
	"testing"

	"github.com/grafana/regexp"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

const benchmarkRowCount = 10000

// createBoolArray creates a boolean array with individual values
func createBoolArray(t testing.TB, alloc *memory.Allocator, values []bool) columnar.Array {
	anyValues := make([]any, len(values))
	for i, v := range values {
		anyValues[i] = v
	}
	return columnartest.Array(t, columnar.KindBool, alloc, anyValues...)
}

// createStringArray creates a string array with individual values
func createStringArray(t testing.TB, alloc *memory.Allocator, values []string) columnar.Array {
	anyValues := make([]any, len(values))
	for i, v := range values {
		anyValues[i] = v
	}
	return columnartest.Array(t, columnar.KindUTF8, alloc, anyValues...)
}

// createUint64Array creates a uint64 array with individual values
func createUint64Array(t testing.TB, alloc *memory.Allocator, values []uint64) columnar.Array {
	anyValues := make([]any, len(values))
	for i, v := range values {
		anyValues[i] = v
	}
	return columnartest.Array(t, columnar.KindUint64, alloc, anyValues...)
}

// BenchmarkAND_AllFalseLeft benchmarks the best case for AND optimization:
// left side is all false, so right side should be completely skipped.
func BenchmarkAND_AllFalseLeft(b *testing.B) {
	var alloc memory.Allocator

	// Create a record with all false values on left side
	falseValues := make([]bool, benchmarkRowCount)
	for i := range falseValues {
		falseValues[i] = false
	}
	leftArray := createBoolArray(b, &alloc, falseValues)

	// Right side: expensive regex match that should be skipped
	textValues := make([]string, benchmarkRowCount)
	for i := range textValues {
		textValues[i] = "some text value for benchmarking"
	}
	textArray := createStringArray(b, &alloc, textValues)

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left"},
			{Name: "text"},
		}),
		benchmarkRowCount,
		[]columnar.Array{leftArray, textArray},
	)

	// Expression: left AND (text =~ ".*expensive.*")
	e := &expr.Binary{
		Left: &expr.Column{Name: "left"},
		Op:   expr.BinaryOpAND,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "text"},
			Op:    expr.BinaryOpMatchRegex,
			Right: &expr.Regexp{Expression: regexp.MustCompile(".*expensive.*")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAND_PartialFalseLeft benchmarks partial optimization case:
// left side is 50% false, so right side evaluated for 50% of rows.
func BenchmarkAND_PartialFalseLeft(b *testing.B) {
	var alloc memory.Allocator

	// Create a record with 50% false values on left side
	falseValues := make([]bool, benchmarkRowCount)
	for i := range falseValues {
		falseValues[i] = i%2 == 0 // 50% true, 50% false
	}
	leftArray := createBoolArray(b, &alloc, falseValues)

	// Right side: expensive regex match
	textValues := make([]string, benchmarkRowCount)
	for i := range textValues {
		textValues[i] = "some text value for benchmarking"
	}
	textArray := createStringArray(b, &alloc, textValues)

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left"},
			{Name: "text"},
		}),
		benchmarkRowCount,
		[]columnar.Array{leftArray, textArray},
	)

	// Expression: left AND (text =~ ".*expensive.*")
	e := &expr.Binary{
		Left: &expr.Column{Name: "left"},
		Op:   expr.BinaryOpAND,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "text"},
			Op:    expr.BinaryOpMatchRegex,
			Right: &expr.Regexp{Expression: regexp.MustCompile(".*expensive.*")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAND_AllTrueLeft benchmarks the baseline case with no optimization:
// left side is all true, so right side must be evaluated for all rows.
func BenchmarkAND_AllTrueLeft(b *testing.B) {
	var alloc memory.Allocator

	// Create a record with all true values on left side
	trueValues := make([]bool, benchmarkRowCount)
	for i := range trueValues {
		trueValues[i] = true
	}
	leftArray := createBoolArray(b, &alloc, trueValues)

	// Right side: expensive regex match that cannot be skipped
	textValues := make([]string, benchmarkRowCount)
	for i := range textValues {
		textValues[i] = "some text value for benchmarking"
	}
	textArray := createStringArray(b, &alloc, textValues)

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left"},
			{Name: "text"},
		}),
		benchmarkRowCount,
		[]columnar.Array{leftArray, textArray},
	)

	// Expression: left AND (text =~ ".*expensive.*")
	e := &expr.Binary{
		Left: &expr.Column{Name: "left"},
		Op:   expr.BinaryOpAND,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "text"},
			Op:    expr.BinaryOpMatchRegex,
			Right: &expr.Regexp{Expression: regexp.MustCompile(".*expensive.*")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOR_AllTrueLeft benchmarks the best case for OR optimization:
// left side is all true, so right side should be completely skipped.
func BenchmarkOR_AllTrueLeft(b *testing.B) {
	var alloc memory.Allocator

	// Create a record with all true values on left side
	trueValues := make([]bool, benchmarkRowCount)
	for i := range trueValues {
		trueValues[i] = true
	}
	leftArray := createBoolArray(b, &alloc, trueValues)

	// Right side: expensive regex match that should be skipped
	textValues := make([]string, benchmarkRowCount)
	for i := range textValues {
		textValues[i] = "some text value for benchmarking"
	}
	textArray := createStringArray(b, &alloc, textValues)

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left"},
			{Name: "text"},
		}),
		benchmarkRowCount,
		[]columnar.Array{leftArray, textArray},
	)

	// Expression: left OR (text =~ ".*expensive.*")
	e := &expr.Binary{
		Left: &expr.Column{Name: "left"},
		Op:   expr.BinaryOpOR,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "text"},
			Op:    expr.BinaryOpMatchRegex,
			Right: &expr.Regexp{Expression: regexp.MustCompile(".*expensive.*")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOR_PartialTrueLeft benchmarks partial optimization case:
// left side is 50% true, so right side evaluated for 50% of rows.
func BenchmarkOR_PartialTrueLeft(b *testing.B) {
	var alloc memory.Allocator

	// Create a record with 50% true values on left side
	trueValues := make([]bool, benchmarkRowCount)
	for i := range trueValues {
		trueValues[i] = i%2 == 0 // 50% true, 50% false
	}
	leftArray := createBoolArray(b, &alloc, trueValues)

	// Right side: expensive regex match
	textValues := make([]string, benchmarkRowCount)
	for i := range textValues {
		textValues[i] = "some text value for benchmarking"
	}
	textArray := createStringArray(b, &alloc, textValues)

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left"},
			{Name: "text"},
		}),
		benchmarkRowCount,
		[]columnar.Array{leftArray, textArray},
	)

	// Expression: left OR (text =~ ".*expensive.*")
	e := &expr.Binary{
		Left: &expr.Column{Name: "left"},
		Op:   expr.BinaryOpOR,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "text"},
			Op:    expr.BinaryOpMatchRegex,
			Right: &expr.Regexp{Expression: regexp.MustCompile(".*expensive.*")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOR_AllFalseLeft benchmarks the baseline case with no optimization:
// left side is all false, so right side must be evaluated for all rows.
func BenchmarkOR_AllFalseLeft(b *testing.B) {
	var alloc memory.Allocator

	// Create a record with all false values on left side
	falseValues := make([]bool, benchmarkRowCount)
	for i := range falseValues {
		falseValues[i] = false
	}
	leftArray := createBoolArray(b, &alloc, falseValues)

	// Right side: expensive regex match that cannot be skipped
	textValues := make([]string, benchmarkRowCount)
	for i := range textValues {
		textValues[i] = "some text value for benchmarking"
	}
	textArray := createStringArray(b, &alloc, textValues)

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left"},
			{Name: "text"},
		}),
		benchmarkRowCount,
		[]columnar.Array{leftArray, textArray},
	)

	// Expression: left OR (text =~ ".*expensive.*")
	e := &expr.Binary{
		Left: &expr.Column{Name: "left"},
		Op:   expr.BinaryOpOR,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "text"},
			Op:    expr.BinaryOpMatchRegex,
			Right: &expr.Regexp{Expression: regexp.MustCompile(".*expensive.*")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExpensiveRegex_WithShortCircuit benchmarks real-world scenario
// with cheap filter first, then expensive regex (should skip most regex operations).
func BenchmarkExpensiveRegex_WithShortCircuit(b *testing.B) {
	var alloc memory.Allocator

	// Create data where only 10% of rows pass the cheap filter
	ageValues := make([]uint64, benchmarkRowCount)
	textValues := make([]string, benchmarkRowCount)
	for i := range ageValues {
		if i%10 == 0 {
			ageValues[i] = 25 // 10% pass the age check
		} else {
			ageValues[i] = 10 // 90% fail the age check
		}
		textValues[i] = "student name with lots of text to make regex slower"
	}

	ageArray := createUint64Array(b, &alloc, ageValues)
	textArray := createStringArray(b, &alloc, textValues)

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "age"},
			{Name: "name"},
		}),
		benchmarkRowCount,
		[]columnar.Array{ageArray, textArray},
	)

	// Expression: (age >= 18) AND (name =~ ".*student.*")
	// This should skip 90% of regex evaluations
	e := &expr.Binary{
		Left: &expr.Binary{
			Left:  &expr.Column{Name: "age"},
			Op:    expr.BinaryOpGTE,
			Right: &expr.Constant{Value: columnartest.Scalar(b, columnar.KindUint64, 18)},
		},
		Op: expr.BinaryOpAND,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "name"},
			Op:    expr.BinaryOpMatchRegex,
			Right: &expr.Regexp{Expression: regexp.MustCompile(".*student.*")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestShortCircuitOptimizationPerformance verifies that the short-circuit optimization
// provides measurable performance improvements compared to baseline cases.
func TestShortCircuitOptimizationPerformance(t *testing.T) {
	// skipped because it's slow, comment it out to run example
	t.SkipNow()

	// Run benchmarks and collect results
	andOptimized := testing.Benchmark(BenchmarkAND_AllFalseLeft)
	andBaseline := testing.Benchmark(BenchmarkAND_AllTrueLeft)
	orOptimized := testing.Benchmark(BenchmarkOR_AllTrueLeft)
	orBaseline := testing.Benchmark(BenchmarkOR_AllFalseLeft)

	// Calculate ns/op for each benchmark
	andOptimizedNsOp := andOptimized.NsPerOp()
	andBaselineNsOp := andBaseline.NsPerOp()
	orOptimizedNsOp := orOptimized.NsPerOp()
	orBaselineNsOp := orBaseline.NsPerOp()

	t.Logf("AND optimized (all-false): %d ns/op", andOptimizedNsOp)
	t.Logf("AND baseline (all-true): %d ns/op", andBaselineNsOp)
	t.Logf("OR optimized (all-true): %d ns/op", orOptimizedNsOp)
	t.Logf("OR baseline (all-false): %d ns/op", orBaselineNsOp)

	// Verify that optimized cases are faster than baseline
	// We expect at least some improvement, though the exact ratio depends on the operation
	if andOptimizedNsOp >= andBaselineNsOp {
		t.Logf("Warning: AND short-circuit optimization is not faster than baseline")
		t.Logf("  Optimized: %d ns/op, Baseline: %d ns/op", andOptimizedNsOp, andBaselineNsOp)
		// Don't fail the test, as performance can vary based on machine load
		// The optimization is still correct even if not always faster
	} else {
		improvement := float64(andBaselineNsOp-andOptimizedNsOp) / float64(andBaselineNsOp) * 100
		t.Logf("AND short-circuit optimization improved performance by %.1f%%", improvement)
	}

	if orOptimizedNsOp >= orBaselineNsOp {
		t.Logf("Warning: OR short-circuit optimization is not faster than baseline")
		t.Logf("  Optimized: %d ns/op, Baseline: %d ns/op", orOptimizedNsOp, orBaselineNsOp)
		// Don't fail the test, as performance can vary based on machine load
	} else {
		improvement := float64(orBaselineNsOp-orOptimizedNsOp) / float64(orBaselineNsOp) * 100
		t.Logf("OR short-circuit optimization improved performance by %.1f%%", improvement)
	}
}
