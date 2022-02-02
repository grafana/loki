package usagestats

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/log"
	"github.com/google/uuid"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/kv"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/client_golang/prometheus"
	prom "github.com/prometheus/prometheus/web/api/v1"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/util/build"
)

const (
	ClusterSeedFileName = "loki_cluster_seed.json"

	// attemptNumber how many times we will try to read a corrupted cluster seed before deleting
	attemptNumber = 4

	seedKey = "usagestats_token"
)

var JSONCodec = jsonCodec{}

type jsonCodec struct{}

func (jsonCodec) Decode(data []byte) (interface{}, error) {
	var seed ClusterSeed
	if err := jsoniter.ConfigFastest.Unmarshal(data, &seed); err != nil {
		return nil, err
	}
	return &seed, nil
}

func (jsonCodec) Encode(obj interface{}) ([]byte, error) {
	return jsoniter.ConfigFastest.Marshal(obj)
}
func (jsonCodec) CodecID() string { return "usagestats.jsonCodec" }

type ClusterSeed struct {
	UID                    string    `json:"UID"`
	CreatedAt              time.Time `json:"created_at"`
	prom.PrometheusVersion `json:"version"`
}

type Reporter struct {
	kvClient     kv.Client
	logger       log.Logger
	objectClient chunk.ObjectClient
	reg          prometheus.Registerer

	cluster *ClusterSeed
}

func NewReporter(kvConfig kv.Config, objectClient chunk.ObjectClient, logger log.Logger, reg prometheus.Registerer) (*Reporter, error) {
	kvClient, err := kv.NewClient(kvConfig, JSONCodec, kv.RegistererWithKVName(reg, "usagestats"), logger)
	if err != nil {
		return nil, err
	}
	return &Reporter{
		kvClient:     kvClient,
		logger:       logger,
		objectClient: objectClient,
		reg:          reg,
	}, nil
}

func (rep *Reporter) initLeader(ctx context.Context) error {
	// Try to become leader via the kv client
	for backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: time.Second,
		MaxBackoff: time.Minute,
		MaxRetries: 0,
	}); ; backoff.Ongoing() {
		// create a new cluster seed
		seed := ClusterSeed{
			UID:               uuid.NewString(),
			PrometheusVersion: build.GetVersion(),
			CreatedAt:         time.Now(),
		}
		if err := rep.kvClient.CAS(ctx, seedKey, func(in interface{}) (out interface{}, retry bool, err error) {
			// The key is already set, so we don't need to do anything
			if in != nil {
				if kvSeed, ok := in.(ClusterSeed); ok && kvSeed.UID != seed.UID {
					seed = in.(ClusterSeed)
					return nil, false, nil
				}
			}
			return seed, true, nil
		}); err != nil {
			level.Error(rep.logger).Log("msg", "failed to CAS cluster seed key", "err", err)
			continue
		}
		// Fetch the remote cluster seed.
		remoteSeed, err := rep.fetchSeed(ctx,
			func(err error) bool {
				// we only want to retry if the error is not an object not found error
				return !rep.objectClient.IsObjectNotFoundErr(err)
			})
		if err != nil {
			if rep.objectClient.IsObjectNotFoundErr(err) {
				// we are the leader and we need to save the file.
				if err := rep.writeSeedFile(ctx, seed); err != nil {
					level.Error(rep.logger).Log("msg", "failed to CAS cluster seed key", "err", err)
					continue
				}
				rep.cluster = &seed
				return nil
			}
			continue
		}
		rep.cluster = remoteSeed
		return nil
	}
}

func (rep *Reporter) fetchSeed(ctx context.Context, continueFn func(err error) bool) (*ClusterSeed, error) {
	var (
		backoff = backoff.New(ctx, backoff.Config{
			MinBackoff: time.Second,
			MaxBackoff: time.Minute,
			MaxRetries: 0,
		})
		readingErr = 0
	)
	for backoff.Ongoing() {
		seed, err := rep.readSeedFile(ctx)
		if err != nil {
			if !rep.objectClient.IsObjectNotFoundErr(err) {
				readingErr++
			}
			level.Error(rep.logger).Log("msg", "failed to read cluster seed file", "err", err)
			if readingErr > attemptNumber {
				if err := rep.objectClient.DeleteObject(ctx, ClusterSeedFileName); err != nil {
					level.Error(rep.logger).Log("msg", "failed to delete corrupted cluster seed file, deleting it", "err", err)
				}
				readingErr = 0
			}
			if continueFn == nil || continueFn(err) {
				continue
			}
			return nil, err
		}
		return seed, nil
	}
	return nil, backoff.Err()
}

func (rep *Reporter) readSeedFile(ctx context.Context) (*ClusterSeed, error) {
	reader, _, err := rep.objectClient.GetObject(ctx, ClusterSeedFileName)
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			level.Error(rep.logger).Log("msg", "failed to close reader", "err", err)
		}
	}()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	seed, err := JSONCodec.Decode(data)
	if err != nil {
		return nil, err
	}
	return seed.(*ClusterSeed), nil
}

func (rep *Reporter) writeSeedFile(ctx context.Context, seed ClusterSeed) error {
	data, err := JSONCodec.Encode(seed)
	if err != nil {
		return err
	}
	return rep.objectClient.PutObject(ctx, ClusterSeedFileName, bytes.NewReader(data))
}
