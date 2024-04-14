package testing

import (
	"github.com/xdire/xlb"
	"time"
)

type TestServicePoolConfig struct {
	SvcIdentity          string
	SvcPort              int
	SvcRateQuotaTimes    int
	SvcRateQuotaDuration time.Duration
	SvcRoutes            []xlb.Route
	Certificate          string
	CertKey              string
}

func (t TestServicePoolConfig) GetCertificate() string {
	return t.Certificate
}

func (t TestServicePoolConfig) GetPrivateKey() string {
	return t.CertKey
}

func (t TestServicePoolConfig) Identity() string {
	return t.SvcIdentity
}

func (t TestServicePoolConfig) Port() int {
	return t.SvcPort
}

func (t TestServicePoolConfig) RateQuota() (int, time.Duration) {
	return t.SvcRateQuotaTimes, t.SvcRateQuotaDuration
}

func (t TestServicePoolConfig) Routes() []xlb.Route {
	return t.SvcRoutes
}

func (t TestServicePoolConfig) TLSData() xlb.TLSData {
	return t
}

func (t TestServicePoolConfig) UnauthorizedAttempts() int {
	//TODO implement me
	panic("implement me")
}

func (t TestServicePoolConfig) HealthCheckValidations() int {
	//TODO implement me
	panic("implement me")
}

func (t TestServicePoolConfig) RouteTimeout() time.Duration {
	return time.Second * 30
}

type TestServicePoolRoute struct {
	ServicePath   string
	ServiceActive bool
}

func (t TestServicePoolRoute) Path() string {
	return t.ServicePath
}

func (t TestServicePoolRoute) Active() bool {
	return t.ServiceActive
}
