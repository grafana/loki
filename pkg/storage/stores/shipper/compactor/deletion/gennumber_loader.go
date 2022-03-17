package deletion

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/util/log"

	"github.com/prometheus/client_golang/prometheus"
)

const reloadDuration = 5 * time.Minute

type GenNumberLoader struct {
	numberGetter genNumberGetter
	numbers      map[string]string
	quit         chan struct{}
	lock         sync.RWMutex
	metrics      *genLoaderMetrics
}

type genNumberGetter interface {
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)
}

type genLoaderMetrics struct {
	cacheGenLoadFailures prometheus.Counter
}

func NewGenNumberLoader(g genNumberGetter, registerer prometheus.Registerer) *GenNumberLoader {
	if g == nil {
		g = &noopNumberGetter{}
	}

	l := &GenNumberLoader{
		numberGetter: g,
		numbers:      make(map[string]string),
		metrics:      newGenLoaderMetrics(registerer),
	}
	go l.loop()

	return l
}

func newGenLoaderMetrics(r prometheus.Registerer) *genLoaderMetrics {
	return &genLoaderMetrics{
		cacheGenLoadFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "delete_cache_gen_load_failures_total",
			Help:      "Total number of failures while loading cache generation number using gen number loader",
		}),
	}
}

func (l *GenNumberLoader) loop() {
	timer := time.NewTicker(reloadDuration)
	for {
		select {
		case <-timer.C:
			err := l.reload()
			if err != nil {
				level.Error(log.Logger).Log("msg", "error reloading tombstones", "err", err)
			}
		case <-l.quit:
			return
		}
	}
}

func (l *GenNumberLoader) reload() error {
	updatedGenNumbers, err := l.getUpdatedGenNumbers()
	if err != nil {
		return err
	}

	l.lock.Lock()
	defer l.lock.Unlock()
	for userID, genNumber := range updatedGenNumbers {
		l.numbers[userID] = genNumber
	}

	return nil
}

func (l *GenNumberLoader) getUpdatedGenNumbers() (map[string]string, error) {
	l.lock.RLock()
	defer l.lock.RUnlock()

	updatedGenNumbers := make(map[string]string)
	for userID, oldGenNumber := range l.numbers {
		genNumber, err := l.numberGetter.GetCacheGenerationNumber(context.Background(), userID)
		if err != nil {
			return nil, err
		}

		if oldGenNumber != genNumber {
			updatedGenNumbers[userID] = genNumber
		}
	}

	return updatedGenNumbers, nil
}

func (l *GenNumberLoader) GetResultsCacheGenNumber(tenantIDs []string) string {
	return l.getCacheGenNumbersPerTenants(tenantIDs)
}

func (l *GenNumberLoader) GetStoreCacheGenNumber(tenantIDs []string) string {
	return l.getCacheGenNumbersPerTenants(tenantIDs)
}

func (l *GenNumberLoader) getCacheGenNumbersPerTenants(tenantIDs []string) string {
	var max int
	for _, tenantID := range tenantIDs {
		genNumber := l.getCacheGenNumber(tenantID)
		if genNumber == "" {
			continue
		}

		number, err := strconv.Atoi(genNumber)
		if err != nil {
			level.Error(log.Logger).Log("msg", "error parsing resultsCacheGenNumber", "user", tenantID, "err", err)
		}

		if number > max {
			max = number
		}
	}

	if max == 0 {
		return ""
	}
	return fmt.Sprint(max)
}

func (l *GenNumberLoader) getCacheGenNumber(userID string) string {
	l.lock.RLock()
	if genNumber, ok := l.numbers[userID]; ok {
		l.lock.RUnlock()
		return genNumber
	}
	l.lock.RUnlock()

	genNumber, err := l.numberGetter.GetCacheGenerationNumber(context.Background(), userID)
	if err != nil {
		level.Error(log.Logger).Log("msg", "error loading cache generation numbers", "err", err)
		l.metrics.cacheGenLoadFailures.Inc()
		return ""
	}

	l.lock.Lock()
	defer l.lock.Unlock()

	l.numbers[userID] = genNumber
	return genNumber
}

func (l *GenNumberLoader) Stop() {
	close(l.quit)
}

type noopNumberGetter struct{}

func (g *noopNumberGetter) GetCacheGenerationNumber(_ context.Context, _ string) (string, error) {
	return "", nil
}
