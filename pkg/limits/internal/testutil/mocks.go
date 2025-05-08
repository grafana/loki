package testutil

type MockLimits struct {
	MaxGlobalStreams int
	IngestionRate    float64
}

func (m *MockLimits) MaxGlobalStreamsPerUser(_ string) int {
	return m.MaxGlobalStreams
}

func (m *MockLimits) IngestionRateBytes(_ string) float64 {
	return m.IngestionRate
}

func (m *MockLimits) IngestionBurstSizeBytes(_ string) int {
	return 1000
}
