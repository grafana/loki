package maintnotifications

import (
	"errors"

	"github.com/redis/go-redis/v9/internal/maintnotifications/logs"
)

// Configuration errors
var (
	ErrInvalidRelaxedTimeout             = errors.New(logs.InvalidRelaxedTimeoutError())
	ErrInvalidHandoffTimeout             = errors.New(logs.InvalidHandoffTimeoutError())
	ErrInvalidHandoffWorkers             = errors.New(logs.InvalidHandoffWorkersError())
	ErrInvalidHandoffQueueSize           = errors.New(logs.InvalidHandoffQueueSizeError())
	ErrInvalidPostHandoffRelaxedDuration = errors.New(logs.InvalidPostHandoffRelaxedDurationError())
	ErrInvalidEndpointType               = errors.New(logs.InvalidEndpointTypeError())
	ErrInvalidMaintNotifications         = errors.New(logs.InvalidMaintNotificationsError())
	ErrMaxHandoffRetriesReached          = errors.New(logs.MaxHandoffRetriesReachedError())

	// Configuration validation errors
	ErrInvalidHandoffRetries = errors.New(logs.InvalidHandoffRetriesError())
)

// Integration errors
var (
	ErrInvalidClient = errors.New(logs.InvalidClientError())
)

// Handoff errors
var (
	ErrHandoffQueueFull = errors.New(logs.HandoffQueueFullError())
)

// Notification errors
var (
	ErrInvalidNotification = errors.New(logs.InvalidNotificationError())
)

// connection handoff errors
var (
	// ErrConnectionMarkedForHandoff is returned when a connection is marked for handoff
	// and should not be used until the handoff is complete
	ErrConnectionMarkedForHandoff = errors.New("" + logs.ConnectionMarkedForHandoffErrorMessage)
	// ErrConnectionInvalidHandoffState is returned when a connection is in an invalid state for handoff
	ErrConnectionInvalidHandoffState = errors.New("" + logs.ConnectionInvalidHandoffStateErrorMessage)
)

// general errors
var (
	ErrShutdown = errors.New(logs.ShutdownError())
)

// circuit breaker errors
var (
	ErrCircuitBreakerOpen = errors.New("" + logs.CircuitBreakerOpenErrorMessage)
)

// circuit breaker configuration errors
var (
	ErrInvalidCircuitBreakerFailureThreshold = errors.New(logs.InvalidCircuitBreakerFailureThresholdError())
	ErrInvalidCircuitBreakerResetTimeout     = errors.New(logs.InvalidCircuitBreakerResetTimeoutError())
	ErrInvalidCircuitBreakerMaxRequests      = errors.New(logs.InvalidCircuitBreakerMaxRequestsError())
)
