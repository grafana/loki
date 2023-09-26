package generationnumber

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetCacheGenNumber(t *testing.T) {
	s := &mockGenNumberClient{
		genNumbers: map[string]string{
			"tenant-a": "1000",
			"tenant-b": "1050",
		},
	}
	loader := NewGenNumberLoader(s, nil)

	for _, tc := range []struct {
		name                          string
		expectedResultsCacheGenNumber string
		tenantIDs                     []string
	}{
		{
			name:                          "single tenant with numeric values",
			tenantIDs:                     []string{"tenant-a"},
			expectedResultsCacheGenNumber: "1000",
		},
		{
			name:                          "multiple tenants with numeric values",
			tenantIDs:                     []string{"tenant-a", "tenant-b"},
			expectedResultsCacheGenNumber: "1050",
		},
		{
			name: "no tenants", // not really an expected call, edge case check to avoid any panics
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectedResultsCacheGenNumber, loader.GetResultsCacheGenNumber(tc.tenantIDs))
		})
	}
}

type mockGenNumberClient struct {
	genNumbers map[string]string
}

func (g *mockGenNumberClient) GetCacheGenerationNumber(_ context.Context, userID string) (string, error) {
	return g.genNumbers[userID], nil
}

func (g *mockGenNumberClient) Name() string {
	return ""
}

func (g *mockGenNumberClient) Stop() {}
