//go:build windows

package device

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestWPDTransportIdentityAndFactory(t *testing.T) {
	transports := platformMTPTransports()
	if len(transports) != 1 || transports[0].ID() != "mtp-wpd" {
		t.Fatalf("platformMTPTransports() = %#v", transports)
	}
}

func TestCopyWithContextCopiesAndStopsOnCancellation(t *testing.T) {
	var dst bytes.Buffer
	n, err := copyWithContext(context.Background(), &dst, bytes.NewBufferString("book"))
	if err != nil || n != 4 || dst.String() != "book" {
		t.Fatalf("copyWithContext() = (%d, %v, %q)", n, err, dst.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := copyWithContext(ctx, &dst, bytes.NewBufferString("ignored")); !errors.Is(err, context.Canceled) {
		t.Fatalf("copyWithContext(cancelled) error = %v", err)
	}
}
