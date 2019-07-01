package ring

// Based on https://raw.githubusercontent.com/stathat/consistent/master/consistent.go

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
)

const (
	unhealthy = "Unhealthy"

	// ConsulKey is the key under which we store the ring in consul.
	ConsulKey = "ring"
)

// ReadRing represents the read inferface to the ring.
type ReadRing interface {
	prometheus.Collector

	Get(key uint32, op Operation) (ReplicationSet, error)
	BatchGet(keys []uint32, op Operation) ([]ReplicationSet, error)
	GetAll() (ReplicationSet, error)
	ReplicationFactor() int
}

// Operation can be Read or Write
type Operation int

// Values for Operation
const (
	Read Operation = iota
	Write
	Reporting // Special value for inquiring about health
)

type uint32s []uint32

func (x uint32s) Len() int           { return len(x) }
func (x uint32s) Less(i, j int) bool { return x[i] < x[j] }
func (x uint32s) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }

// ErrEmptyRing is the error returned when trying to get an element when nothing has been added to hash.
var ErrEmptyRing = errors.New("empty ring")

// Config for a Ring
type Config struct {
	KVStore           KVConfig      `yaml:"kvstore,omitempty"`
	HeartbeatTimeout  time.Duration `yaml:"heartbeat_timeout,omitempty"`
	ReplicationFactor int           `yaml:"replication_factor,omitempty"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet with a specified prefix
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet with a specified prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.KVStore.RegisterFlagsWithPrefix(prefix, f)

	f.DurationVar(&cfg.HeartbeatTimeout, prefix+"ring.heartbeat-timeout", time.Minute, "The heartbeat timeout after which ingesters are skipped for reads/writes.")
	f.IntVar(&cfg.ReplicationFactor, prefix+"distributor.replication-factor", 3, "The number of ingesters to write to and read from.")
}

// Ring holds the information about the members of the consistent hash ring.
type Ring struct {
	name     string
	cfg      Config
	KVClient KVClient
	done     chan struct{}
	quit     context.CancelFunc

	mtx      sync.RWMutex
	ringDesc *Desc

	memberOwnershipDesc *prometheus.Desc
	numMembersDesc      *prometheus.Desc
	totalTokensDesc     *prometheus.Desc
	numTokensDesc       *prometheus.Desc
}

// New creates a new Ring
func New(cfg Config, name string) (*Ring, error) {
	if cfg.ReplicationFactor <= 0 {
		return nil, fmt.Errorf("ReplicationFactor must be greater than zero: %d", cfg.ReplicationFactor)
	}
	codec := ProtoCodec{Factory: ProtoDescFactory}
	store, err := NewKVStore(cfg.KVStore, codec)
	if err != nil {
		return nil, err
	}

	r := &Ring{
		name:     name,
		cfg:      cfg,
		KVClient: store,
		done:     make(chan struct{}),
		ringDesc: &Desc{},
		memberOwnershipDesc: prometheus.NewDesc(
			"cortex_ring_member_ownership_percent",
			"The percent ownership of the ring by member",
			[]string{"member", "name"}, nil,
		),
		numMembersDesc: prometheus.NewDesc(
			"cortex_ring_members",
			"Number of members in the ring",
			[]string{"state", "name"}, nil,
		),
		totalTokensDesc: prometheus.NewDesc(
			"cortex_ring_tokens_total",
			"Number of tokens in the ring",
			[]string{"name"}, nil,
		),
		numTokensDesc: prometheus.NewDesc(
			"cortex_ring_tokens_owned",
			"The number of tokens in the ring owned by the member",
			[]string{"member", "name"}, nil,
		),
	}
	var ctx context.Context
	ctx, r.quit = context.WithCancel(context.Background())
	go r.loop(ctx)
	return r, nil
}

// Stop the distributor.
func (r *Ring) Stop() {
	r.quit()
	<-r.done
}

func (r *Ring) loop(ctx context.Context) {
	defer close(r.done)
	r.KVClient.WatchKey(ctx, ConsulKey, func(value interface{}) bool {
		if value == nil {
			level.Info(util.Logger).Log("msg", "ring doesn't exist in consul yet")
			return true
		}

		ringDesc := value.(*Desc)
		ringDesc.Tokens = migrateRing(ringDesc)
		r.mtx.Lock()
		defer r.mtx.Unlock()
		r.ringDesc = ringDesc
		return true
	})
}

// migrateRing will denormalise the ring's tokens if stored in normal form.
func migrateRing(desc *Desc) []TokenDesc {
	numTokens := len(desc.Tokens)
	for _, ing := range desc.Ingesters {
		numTokens += len(ing.Tokens)
	}
	tokens := make([]TokenDesc, len(desc.Tokens), numTokens)
	copy(tokens, desc.Tokens)
	for key, ing := range desc.Ingesters {
		for _, token := range ing.Tokens {
			tokens = append(tokens, TokenDesc{
				Token:    token,
				Ingester: key,
			})
		}
	}
	sort.Sort(ByToken(tokens))
	return tokens
}

// Get returns n (or more) ingesters which form the replicas for the given key.
func (r *Ring) Get(key uint32, op Operation) (ReplicationSet, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.getInternal(key, op)
}

// BatchGet returns ReplicationFactor (or more) ingesters which form the replicas
// for the given keys. The order of the result matches the order of the input.
func (r *Ring) BatchGet(keys []uint32, op Operation) ([]ReplicationSet, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	result := make([]ReplicationSet, len(keys), len(keys))
	for i, key := range keys {
		rs, err := r.getInternal(key, op)
		if err != nil {
			return nil, err
		}
		result[i] = rs
	}
	return result, nil
}

func (r *Ring) getInternal(key uint32, op Operation) (ReplicationSet, error) {
	if r.ringDesc == nil || len(r.ringDesc.Tokens) == 0 {
		return ReplicationSet{}, ErrEmptyRing
	}

	var (
		n             = r.cfg.ReplicationFactor
		ingesters     = make([]IngesterDesc, 0, n)
		distinctHosts = map[string]struct{}{}
		start         = r.search(key)
		iterations    = 0
	)
	for i := start; len(distinctHosts) < n && iterations < len(r.ringDesc.Tokens); i++ {
		iterations++
		// Wrap i around in the ring.
		i %= len(r.ringDesc.Tokens)

		// We want n *distinct* ingesters.
		token := r.ringDesc.Tokens[i]
		if _, ok := distinctHosts[token.Ingester]; ok {
			continue
		}
		distinctHosts[token.Ingester] = struct{}{}
		ingester := r.ringDesc.Ingesters[token.Ingester]

		// We do not want to Write to Ingesters that are not ACTIVE, but we do want
		// to write the extra replica somewhere.  So we increase the size of the set
		// of replicas for the key. This means we have to also increase the
		// size of the replica set for read, but we can read from Leaving ingesters,
		// so don't skip it in this case.
		// NB dead ingester will be filtered later (by replication_strategy.go).
		if op == Write && ingester.State != ACTIVE {
			n++
		} else if op == Read && (ingester.State != ACTIVE && ingester.State != LEAVING) {
			n++
		}

		ingesters = append(ingesters, ingester)
	}

	liveIngesters, maxFailure, err := r.replicationStrategy(ingesters, op)
	if err != nil {
		return ReplicationSet{}, err
	}

	return ReplicationSet{
		Ingesters: liveIngesters,
		MaxErrors: maxFailure,
	}, nil
}

// GetAll returns all available ingesters in the ring.
func (r *Ring) GetAll() (ReplicationSet, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	if r.ringDesc == nil || len(r.ringDesc.Tokens) == 0 {
		return ReplicationSet{}, ErrEmptyRing
	}

	ingesters := make([]IngesterDesc, 0, len(r.ringDesc.Ingesters))
	maxErrors := r.cfg.ReplicationFactor / 2

	for _, ingester := range r.ringDesc.Ingesters {
		if !r.IsHealthy(&ingester, Read) {
			maxErrors--
			continue
		}
		ingesters = append(ingesters, ingester)
	}

	if maxErrors < 0 {
		return ReplicationSet{}, fmt.Errorf("too many failed ingesters")
	}

	return ReplicationSet{
		Ingesters: ingesters,
		MaxErrors: maxErrors,
	}, nil
}

func (r *Ring) search(key uint32) int {
	i := sort.Search(len(r.ringDesc.Tokens), func(x int) bool {
		return r.ringDesc.Tokens[x].Token > key
	})
	if i >= len(r.ringDesc.Tokens) {
		i = 0
	}
	return i
}

// Describe implements prometheus.Collector.
func (r *Ring) Describe(ch chan<- *prometheus.Desc) {
	ch <- r.memberOwnershipDesc
	ch <- r.numMembersDesc
	ch <- r.totalTokensDesc
	ch <- r.numTokensDesc
}

func countTokens(ringDesc *Desc) (map[string]uint32, map[string]uint32) {
	tokens := ringDesc.Tokens

	owned := map[string]uint32{}
	numTokens := map[string]uint32{}
	for i, token := range tokens {
		var diff uint32
		if i+1 == len(tokens) {
			diff = (math.MaxUint32 - token.Token) + tokens[0].Token
		} else {
			diff = tokens[i+1].Token - token.Token
		}
		numTokens[token.Ingester] = numTokens[token.Ingester] + 1
		owned[token.Ingester] = owned[token.Ingester] + diff
	}

	for id := range ringDesc.Ingesters {
		if _, ok := owned[id]; !ok {
			owned[id] = 0
			numTokens[id] = 0
		}
	}

	return numTokens, owned
}

// Collect implements prometheus.Collector.
func (r *Ring) Collect(ch chan<- prometheus.Metric) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	numTokens, ownedRange := countTokens(r.ringDesc)
	for id, totalOwned := range ownedRange {
		ch <- prometheus.MustNewConstMetric(
			r.memberOwnershipDesc,
			prometheus.GaugeValue,
			float64(totalOwned)/float64(math.MaxUint32),
			id,
			r.name,
		)
		ch <- prometheus.MustNewConstMetric(
			r.numTokensDesc,
			prometheus.GaugeValue,
			float64(numTokens[id]),
			id,
			r.name,
		)
	}

	// Initialised to zero so we emit zero-metrics (instead of not emitting anything)
	byState := map[string]int{
		unhealthy:        0,
		ACTIVE.String():  0,
		LEAVING.String(): 0,
		PENDING.String(): 0,
		JOINING.String(): 0,
	}
	for _, ingester := range r.ringDesc.Ingesters {
		if !r.IsHealthy(&ingester, Reporting) {
			byState[unhealthy]++
		} else {
			byState[ingester.State.String()]++
		}
	}

	for state, count := range byState {
		ch <- prometheus.MustNewConstMetric(
			r.numMembersDesc,
			prometheus.GaugeValue,
			float64(count),
			state,
			r.name,
		)
	}
	ch <- prometheus.MustNewConstMetric(
		r.totalTokensDesc,
		prometheus.GaugeValue,
		float64(len(r.ringDesc.Tokens)),
		r.name,
	)
}
