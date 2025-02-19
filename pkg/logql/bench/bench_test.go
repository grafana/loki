package bench

import "testing"

func BenchmarkLogQL(b *testing.B) {
	builder := newTestDataBuilder(b, "test-tenant")
}
