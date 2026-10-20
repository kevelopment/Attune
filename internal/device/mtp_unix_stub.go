//go:build (darwin || linux) && !libmtp

package device

import (
	"context"
	"errors"
)

var errMTPBindingUnavailable = errors.New("Unix MTP support requires the optional libmtp build; see docs/mtp-unix.md")

type unavailableMTPBackend struct{}

func newUnixMTPBackend() unixMTPBackend { return unavailableMTPBackend{} }
func (unavailableMTPBackend) Discover(context.Context) ([]TransportDevice, error) {
	return nil, errMTPBindingUnavailable
}
func (unavailableMTPBackend) Open(context.Context, string) (unixMTPConnection, error) {
	return nil, errMTPBindingUnavailable
}

func platformMTPTransports() []DeviceTransport { return nil }
