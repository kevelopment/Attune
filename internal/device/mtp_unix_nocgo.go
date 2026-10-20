//go:build (darwin || linux) && !cgo && libmtp

package device

import (
	"context"
	"errors"
)

type noCGOMTPBackend struct{}

func (noCGOMTPBackend) Discover(context.Context) ([]TransportDevice, error) {
	return nil, errors.New("libmtp build requires cgo")
}
func (noCGOMTPBackend) Open(context.Context, string) (unixMTPConnection, error) {
	return nil, errors.New("libmtp build requires cgo")
}
func newUnixMTPBackend() unixMTPBackend        { return noCGOMTPBackend{} }
func platformMTPTransports() []DeviceTransport { return nil }
