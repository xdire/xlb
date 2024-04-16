package xlb

import (
	"time"
)

type ServicePoolConfig struct {
	SvcIdentity               string
	SvcPort                   int
	SvcRateQuotaTimes         int
	SvcRateQuotaDuration      time.Duration
	SvcRoutes                 []Route
	Certificate               string
	CertKey                   string
	SvcHealthCheckValidations int
}

func (t ServicePoolConfig) GetCertificate() string {
	return t.Certificate
}

func (t ServicePoolConfig) GetPrivateKey() string {
	return t.CertKey
}

func (t ServicePoolConfig) Identity() string {
	return t.SvcIdentity
}

func (t ServicePoolConfig) Port() int {
	return t.SvcPort
}

func (t ServicePoolConfig) RateQuota() (int, time.Duration) {
	return t.SvcRateQuotaTimes, t.SvcRateQuotaDuration
}

func (t ServicePoolConfig) Routes() []Route {
	return t.SvcRoutes
}

func (t ServicePoolConfig) TLSData() TLSData {
	return t
}

func (t ServicePoolConfig) UnauthorizedAttempts() int {
	//TODO implement me
	panic("implement me")
}

func (t ServicePoolConfig) HealthCheckValidations() int {
	return t.SvcHealthCheckValidations
}

func (t ServicePoolConfig) RouteTimeout() time.Duration {
	return time.Second * 30
}

type ServicePoolRoute struct {
	ServicePath   string
	ServiceActive bool
}

func (t ServicePoolRoute) Path() string {
	return t.ServicePath
}

func (t ServicePoolRoute) Active() bool {
	return t.ServiceActive
}
