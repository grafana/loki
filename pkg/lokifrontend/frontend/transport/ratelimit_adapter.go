package transport

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/NVIDIA/go-ratelimit/pkg/logger"
	"github.com/NVIDIA/go-ratelimit/pkg/ratelimit"
	"github.com/NVIDIA/go-ratelimit/pkg/utils"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

// RateLimitConfig holds configuration for integrating the go-ratelimit SDK
type RateLimitConfig struct {
	// Enable enables rate limiting for queries
	Enable bool `yaml:"enable"`

	// Redis configuration
	RedisAddress  string        `yaml:"redis_address"`
	RedisPassword string        `yaml:"redis_password"`
	RedisDB       int           `yaml:"redis_db"`
	RedisTimeout  time.Duration `yaml:"redis_timeout"`
	RedisPoolSize int           `yaml:"redis_pool_size"`

	// Default rate limits
	DefaultRequestLimit int           `yaml:"default_request_limit"`
	DefaultByteLimit    int64         `yaml:"default_byte_limit"`
	DefaultWindow       time.Duration `yaml:"default_window"`

	// Rate limit key prefix
	Prefix string `yaml:"prefix"`

	// Rate limit by tenant ID
	RateLimitByTenant bool `yaml:"rate_limit_by_tenant"`

	// Rate limit by IP if tenant not available
	FallbackToIP bool `yaml:"fallback_to_ip"`
}

// RegisterFlags registers rate limit configuration flags
func (cfg *RateLimitConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("rate-limit.", f)
}

// RegisterFlagsWithPrefix registers rate limit configuration flags with a prefix
func (cfg *RateLimitConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enable, prefix+"enable", false, "Enable rate limiting for queries")
	f.StringVar(&cfg.RedisAddress, prefix+"redis-address", "localhost:6379", "Redis server address for rate limiting")
	f.StringVar(&cfg.RedisPassword, prefix+"redis-password", "", "Redis password")
	f.IntVar(&cfg.RedisDB, prefix+"redis-db", 0, "Redis database number")
	f.DurationVar(&cfg.RedisTimeout, prefix+"redis-timeout", 200*time.Millisecond, "Redis operation timeout")
	f.IntVar(&cfg.RedisPoolSize, prefix+"redis-pool-size", 64, "Redis connection pool size")

	f.IntVar(&cfg.DefaultRequestLimit, prefix+"default-request-limit", 100, "Default request limit per window")
	f.Int64Var(&cfg.DefaultByteLimit, prefix+"default-byte-limit", 10*1024*1024, "Default byte limit per window (10MB)")
	f.DurationVar(&cfg.DefaultWindow, prefix+"default-window", time.Minute, "Default rate limit window")
	f.StringVar(&cfg.Prefix, prefix+"prefix", "loki:ratelimit", "Rate limit key prefix in Redis")

	f.BoolVar(&cfg.RateLimitByTenant, prefix+"by-tenant", true, "Rate limit by tenant ID")
	f.BoolVar(&cfg.FallbackToIP, prefix+"fallback-to-ip", true, "Fallback to IP-based rate limiting if tenant ID not available")
}

// Validate validates the rate limit configuration
func (cfg *RateLimitConfig) Validate() error {
	if cfg.Enable {
		if cfg.RedisAddress == "" {
			return fmt.Errorf("redis address is required when rate limiting is enabled")
		}
		if cfg.DefaultRequestLimit <= 0 {
			return fmt.Errorf("default request limit must be positive")
		}
		if cfg.DefaultByteLimit <= 0 {
			return fmt.Errorf("default byte limit must be positive")
		}
		if cfg.DefaultWindow <= 0 {
			return fmt.Errorf("default window must be positive")
		}
	}
	return nil
}

// RateLimitAdapter adapts the go-ratelimit SDK for use with Loki
type RateLimitAdapter struct {
	cfg     RateLimitConfig
	service *ratelimit.Service
	redis   *redis.Client
	logger  log.Logger
}

// NewRateLimitAdapter creates a new rate limit adapter
func NewRateLimitAdapter(cfg RateLimitConfig, lokiLogger log.Logger) (*RateLimitAdapter, error) {
	if !cfg.Enable {
		return &RateLimitAdapter{cfg: cfg, logger: lokiLogger}, nil
	}

	// Initialize the SDK's logger directly to avoid file logging issues
	// The SDK's InitLogger always tries to open a file, so we bypass it
	if logger.Log == nil {
		zapLogger, _ := zap.NewProduction()
		logger.Log = otelzap.New(zapLogger)
	}

	// Create Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddress,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		ReadTimeout:  cfg.RedisTimeout,
		WriteTimeout: cfg.RedisTimeout,
		PoolSize:     cfg.RedisPoolSize,
	})

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	level.Info(lokiLogger).Log("msg", "Connected to Redis for rate limiting", "address", cfg.RedisAddress)

	// Create key function that integrates with Loki's tenant system
	keyFunc := createLokiKeyFunc(cfg, lokiLogger)

	// Use the SDK's ratelimit.Service with our configuration
	options := ratelimit.Options{
		Redis:        rdb,
		Prefix:       cfg.Prefix,
		RequestLimit: cfg.DefaultRequestLimit,
		ByteLimit:    cfg.DefaultByteLimit,
		Window:       cfg.DefaultWindow,
		KeyFunc:      keyFunc,
	}

	service := ratelimit.New(options)

	return &RateLimitAdapter{
		cfg:     cfg,
		service: service,
		redis:   rdb,
		logger:  lokiLogger,
	}, nil
}

// createLokiKeyFunc creates a key extraction function that integrates with Loki's tenant system
func createLokiKeyFunc(cfg RateLimitConfig, logger log.Logger) func(*http.Request) string {
	return func(r *http.Request) string {
		// Try to get tenant ID if configured
		if cfg.RateLimitByTenant {
			ctx := r.Context()
			tenantID, err := tenant.TenantID(ctx)
			if err == nil && tenantID != "" {
				// Use apikey format so the SDK's extractClientID can properly extract the tenant ID
				// Format: "apikey:tenant-{tenantID}|{method}:{path}"
				key := fmt.Sprintf("apikey:tenant-%s|%s:%s", tenantID, r.Method, r.URL.Path)
				level.Debug(logger).Log("msg", "Rate limit key by tenant", "key", key)
				return key
			}
			level.Debug(logger).Log("msg", "Failed to get tenant ID for rate limiting", "error", err)
		}

		// Fallback to IP-based rate limiting if configured
		if cfg.FallbackToIP {
			clientIP := utils.ClientIP(r)
			key := fmt.Sprintf("ip:%s:%s:%s", clientIP, r.Method, r.URL.Path)
			level.Debug(logger).Log("msg", "Rate limit key by IP", "key", key)
			return key
		}

		// Default: use a generic key
		key := fmt.Sprintf("default:%s:%s", r.Method, r.URL.Path)
		level.Debug(logger).Log("msg", "Rate limit key default", "key", key)
		return key
	}
}

// Middleware returns the rate limiting middleware from the SDK
func (a *RateLimitAdapter) Middleware() func(http.Handler) http.Handler {
	if !a.cfg.Enable || a.service == nil {
		// Return a no-op middleware if rate limiting is disabled
		return func(next http.Handler) http.Handler {
			return next
		}
	}
	// Use the SDK's middleware directly
	return a.service.Middleware()
}

// QueryAPIMiddleware returns a rate limiting middleware for query APIs
func (a *RateLimitAdapter) QueryAPIMiddleware() func(http.Handler) http.Handler {
	if !a.cfg.Enable || a.service == nil {
		// Return a no-op middleware if rate limiting is disabled
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	// For now, use the regular middleware instead of selective
	// This will apply rate limiting to all paths
	return a.service.Middleware()
}

// SetTenantLimits sets custom rate limits for a specific tenant using the SDK
func (a *RateLimitAdapter) SetTenantLimits(tenantID string, requestLimit int, byteLimit int64) error {
	if !a.cfg.Enable || a.service == nil {
		return nil
	}

	windowSecs := int(a.cfg.DefaultWindow.Seconds())
	// Use the same client ID format as the key function
	return a.service.SetClientConfig(fmt.Sprintf("tenant-%s", tenantID), requestLimit, byteLimit, windowSecs)
}

// ResetTenantLimits resets a tenant to default rate limits using the SDK
func (a *RateLimitAdapter) ResetTenantLimits(tenantID string) error {
	if !a.cfg.Enable || a.service == nil {
		return nil
	}
	// Use the same client ID format as the key function
	return a.service.ResetClientConfig(fmt.Sprintf("tenant-%s", tenantID))
}

// Close closes the rate limiter connections
func (a *RateLimitAdapter) Close() error {
	if a.redis != nil {
		return a.redis.Close()
	}
	return nil
}
