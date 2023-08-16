package log

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/time/rate"
)

const (
	infoLevel  = "info"
	debugLevel = "debug"
	warnLevel  = "warning"
	errorLevel = "error"
)

type RateLimitedLogger struct {
	next    Interface
	limiter *rate.Limiter

	discardedInfoLogLinesCounter  prometheus.Counter
	discardedDebugLogLinesCounter prometheus.Counter
	discardedWarnLogLinesCounter  prometheus.Counter
	discardedErrorLogLinesCounter prometheus.Counter
}

// NewRateLimitedLogger returns a logger.Interface that is limited to the given number of logs per second,
// with the given burst size.
func NewRateLimitedLogger(logger Interface, logsPerSecond rate.Limit, burstSize int, reg prometheus.Registerer) Interface {
	discardedLogLinesCounter := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "logger_rate_limit_discarded_log_lines_total",
		Help: "Total number of discarded log lines per level.",
	}, []string{"level"})

	return &RateLimitedLogger{
		next:                          logger,
		limiter:                       rate.NewLimiter(logsPerSecond, burstSize),
		discardedInfoLogLinesCounter:  discardedLogLinesCounter.WithLabelValues(infoLevel),
		discardedDebugLogLinesCounter: discardedLogLinesCounter.WithLabelValues(debugLevel),
		discardedWarnLogLinesCounter:  discardedLogLinesCounter.WithLabelValues(warnLevel),
		discardedErrorLogLinesCounter: discardedLogLinesCounter.WithLabelValues(errorLevel),
	}
}

func (l *RateLimitedLogger) Debugf(format string, args ...interface{}) {
	if l.limiter.Allow() {
		l.next.Debugf(format, args...)
	} else {
		l.discardedDebugLogLinesCounter.Inc()
	}
}

func (l *RateLimitedLogger) Debugln(args ...interface{}) {
	if l.limiter.Allow() {
		l.next.Debugln(args...)
	} else {
		l.discardedDebugLogLinesCounter.Inc()
	}
}

func (l *RateLimitedLogger) Infof(format string, args ...interface{}) {
	if l.limiter.Allow() {
		l.next.Infof(format, args...)
	} else {
		l.discardedInfoLogLinesCounter.Inc()
	}
}

func (l *RateLimitedLogger) Infoln(args ...interface{}) {
	if l.limiter.Allow() {
		l.next.Infoln(args...)
	} else {
		l.discardedInfoLogLinesCounter.Inc()
	}
}

func (l *RateLimitedLogger) Errorf(format string, args ...interface{}) {
	if l.limiter.Allow() {
		l.next.Errorf(format, args...)
	} else {
		l.discardedErrorLogLinesCounter.Inc()
	}
}

func (l *RateLimitedLogger) Errorln(args ...interface{}) {
	if l.limiter.Allow() {
		l.next.Errorln(args...)
	} else {
		l.discardedErrorLogLinesCounter.Inc()
	}
}

func (l *RateLimitedLogger) Warnf(format string, args ...interface{}) {
	if l.limiter.Allow() {
		l.next.Warnf(format, args...)
	} else {
		l.discardedWarnLogLinesCounter.Inc()
	}
}

func (l *RateLimitedLogger) Warnln(args ...interface{}) {
	if l.limiter.Allow() {
		l.next.Warnln(args...)
	} else {
		l.discardedWarnLogLinesCounter.Inc()
	}
}

func (l *RateLimitedLogger) WithField(key string, value interface{}) Interface {
	return &RateLimitedLogger{
		next:                          l.next.WithField(key, value),
		limiter:                       l.limiter,
		discardedInfoLogLinesCounter:  l.discardedInfoLogLinesCounter,
		discardedDebugLogLinesCounter: l.discardedDebugLogLinesCounter,
		discardedWarnLogLinesCounter:  l.discardedWarnLogLinesCounter,
		discardedErrorLogLinesCounter: l.discardedErrorLogLinesCounter,
	}
}

func (l *RateLimitedLogger) WithFields(f Fields) Interface {
	return &RateLimitedLogger{
		next:                          l.next.WithFields(f),
		limiter:                       l.limiter,
		discardedInfoLogLinesCounter:  l.discardedInfoLogLinesCounter,
		discardedDebugLogLinesCounter: l.discardedDebugLogLinesCounter,
		discardedWarnLogLinesCounter:  l.discardedWarnLogLinesCounter,
		discardedErrorLogLinesCounter: l.discardedErrorLogLinesCounter,
	}
}
