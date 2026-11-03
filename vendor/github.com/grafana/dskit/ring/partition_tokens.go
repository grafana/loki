package ring

import (
	"fmt"
	"slices"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PartitionTokenGenerator derives sorted tokens from a partition ID.
// Returned tokens are immutable: callers must not change elements or sort them in place.
// Implementations must be safe for concurrent use.
type PartitionTokenGenerator interface {
	TokensFor(id int32) (Tokens, error)
}

var _ PartitionTokenGenerator = (*PartitionTokenTable)(nil)

// PartitionTokenTable provides read-only access to derived tokens generated in NewPartitionTokenTable.
// Share one table across the process's partition rings. Returned slices must not be modified.
type PartitionTokenTable struct {
	tokens []Tokens
}

// NewPartitionTokenTable generates tokens for partition IDs 0 to partitions-1 before it returns,
// so its cost grows with partitions. Zero creates an empty table.
func NewPartitionTokenTable(partitions int32, logger log.Logger, reg prometheus.Registerer) (*PartitionTokenTable, error) {
	return newPartitionTokenTable(partitions, logger, reg, generatePartitionTokens)
}

func newPartitionTokenTable(partitions int32, logger log.Logger, reg prometheus.Registerer, generate func(int32) ([]Tokens, error)) (*PartitionTokenTable, error) {
	if partitions < 0 {
		return nil, fmt.Errorf("derived token partition count must be non-negative, got %d", partitions)
	}
	if partitions == 0 {
		registerPartitionTokenTableMetrics(partitions, reg)
		return &PartitionTokenTable{}, nil
	}
	start := time.Now()
	maxID := partitions - 1
	tokens, err := generate(maxID)
	if err != nil {
		return nil, fmt.Errorf("generate partition tokens through ID %d: %w", maxID, err)
	}
	for id, partitionTokens := range tokens {
		// Every ring shares these slices. Trim capacity to length, so append on a returned slice
		// always copies it instead of writing into the shared array.
		tokens[id] = partitionTokens[:len(partitionTokens):len(partitionTokens)]
	}
	level.Info(logger).Log("msg", "generated partition tokens", "partitions", partitions, "duration", time.Since(start))
	registerPartitionTokenTableMetrics(partitions, reg)
	return &PartitionTokenTable{tokens: tokens}, nil
}

func registerPartitionTokenTableMetrics(partitions int32, reg prometheus.Registerer) {
	promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Name: "partition_ring_max_derived_token_partitions",
		Help: "Number of partition IDs, from 0, that can use derived tokens.",
	}).Set(float64(partitions))
}

// TokensFor returns immutable tokens for id, which must be in [0, the configured partition count).
func (t *PartitionTokenTable) TokensFor(id int32) (Tokens, error) {
	if id < 0 || int(id) >= len(t.tokens) {
		return nil, fmt.Errorf("partition ID %d must be between 0 and %d (exclusive)", id, len(t.tokens))
	}
	return t.tokens[id], nil
}

// resolveRingTokens returns the sorted tokens of all partitions and the partition owning each token.
func resolveRingTokens(desc PartitionRingDesc, opts PartitionRingOptions) (Tokens, map[Token]int32, error) {
	ringTokens := make(Tokens, 0, len(desc.Partitions)*optimalTokensPerInstance)
	partitionByToken := make(map[Token]int32, len(desc.Partitions)*optimalTokensPerInstance)
	for id := range desc.Partitions {
		tokens, err := resolvePartitionTokens(desc, id, opts)
		if err != nil {
			return nil, nil, err
		}
		ringTokens = append(ringTokens, tokens...)
		for _, token := range tokens {
			partitionByToken[Token(token)] = id
		}
	}
	slices.Sort(ringTokens)
	return ringTokens, partitionByToken, nil
}

// resolvePartitionTokens returns the immutable tokens of a partition.
func resolvePartitionTokens(desc PartitionRingDesc, id int32, opts PartitionRingOptions) (Tokens, error) {
	partition, exists := desc.Partitions[id]
	if !exists {
		return nil, ErrPartitionDoesNotExist
	}
	switch partition.TokenScheme {
	case PartitionTokensStored:
		// Use the stored tokens, even when empty.
		return partition.Tokens, nil
	case PartitionTokensSmt512:
		// The scheme is authoritative even when stored tokens are present.
	default:
		return nil, fmt.Errorf("cannot resolve tokens for partition %d: unknown token scheme %d", id, partition.TokenScheme)
	}
	if opts.TokenGenerator == nil {
		return nil, fmt.Errorf("cannot resolve tokens for partition %d: ring has no token generator", id)
	}
	tokens, err := opts.TokenGenerator.TokensFor(id)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve tokens for partition %d: %w", id, err)
	}
	return tokens, nil
}

// generatePartitionTokens returns sorted, deterministic tokens for every ID from 0 through maxPartitionID.
func generatePartitionTokens(maxPartitionID int32) ([]Tokens, error) {
	if maxPartitionID < 0 {
		return nil, fmt.Errorf("partition ID must be non-negative, got %d", maxPartitionID)
	}
	generator := NewSpreadMinimizingTokenGeneratorForInstanceAndZoneID("", int(maxPartitionID), 0, false)
	tokensByID, err := generator.generateTokensByInstanceID()
	if err != nil {
		return nil, err
	}
	tokens := make([]Tokens, int(maxPartitionID)+1)
	for id := range tokens {
		tokens[id] = tokensByID[id]
		slices.Sort(tokens[id])
	}
	return tokens, nil
}
