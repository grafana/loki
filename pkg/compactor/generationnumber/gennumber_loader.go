package generationnumber

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/util/log"
)

const reloadDuration = 5 * time.Minute

type CacheGenClient interface {
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)
	Name() string
	Stop()
}

type GenNumberLoader struct {
	numberGetter CacheGenClient
	numbers      map[string]string
	quit         chan struct{}
	lock         sync.RWMutex
	metrics      *genLoaderMetrics
}

func NewGenNumberLoader(g CacheGenClient, registerer prometheus.Registerer) *GenNumberLoader {
	if g == nil {
		g = &noopNumberGetter{}
	}

	l := &GenNumberLoader{
		numberGetter: g,
		numbers:      make(map[string]string),
		metrics:      newGenLoaderMetrics(registerer),
		quit:         make(chan struct{}),
	}
	go l.loop()

	return l
}

func (l *GenNumberLoader) loop() {
	timer := time.NewTicker(reloadDuration)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			err := l.reload()
			if err != nil {
				level.Error(log.Logger).Log("msg", "error reloading generation numbers", "err", err)
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
		l.metrics.cacheGenLoadFailures.WithLabelValues(l.numberGetter.Name()).Inc()
		return ""
	}

	l.lock.Lock()
	defer l.lock.Unlock()

	l.numbers[userID] = genNumber
	return genNumber
}

func (l *GenNumberLoader) Stop() {
	close(l.quit)
	l.numberGetter.Stop()
}

type noopNumberGetter struct{}

func (g *noopNumberGetter) GetCacheGenerationNumber(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (g *noopNumberGetter) Name() string {
	return "noop-getter"
}

func (g *noopNumberGetter) Stop() {}
