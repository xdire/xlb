package xlb

import "time"

type ServicePool interface {
	// Identity How each ServicePool identified, CN match
	Identity() string
	// Port to listen for incoming traffic
	Port() int
	// RateQuota Rate of times per time.Duration
	RateQuota() (int, time.Duration)
	// Routes to route
	Routes() []Route
	// TLSData to service the frontend
	TLSData() TLSData
	// UnauthorizedAttempts How many unauthorized attempts before IP cache placement
	UnauthorizedAttempts() int
	// HealthCheckValidations Bring host back in routable healthy state after this amount of validations
	HealthCheckValidations() int
	// RouteTimeout general timeout for route to be connected or dropped
	RouteTimeout() time.Duration
}

type Route interface {
	// Path Stores path of the upstream
	Path() string
	// Active Provides information if route is active, in case of update
	// function can provide false and that will adjust behavior of forwarder
	Active() bool
}

type TLSData interface {
	GetCertificate() string
	GetPrivateKey() string
}

type Logger interface {
	Info(s string)
	Error(s string)
	Debug(s string)
}
