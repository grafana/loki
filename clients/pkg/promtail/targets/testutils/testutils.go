package testutils

import (
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
)

var randomGenerator *rand.Rand

func InitRandom() {
	randomGenerator = rand.New(rand.NewSource(time.Now().UnixNano()))
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandName() string {
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[randomGenerator.Intn(len(letters))] //#nosec G404 -- Generating random test data, fine. -- nosemgrep: math-random-used
	}
	return string(b)
}

func ValidateRelabelConfig(t *testing.T, configs []*relabel.Config) []*relabel.Config {
	t.Helper()

	for _, c := range configs {
		require.NoError(t, c.Validate(model.UTF8Validation))
	}

	return configs
}
