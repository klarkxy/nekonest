package ws

import (
	"testing"

	"github.com/nekonest/server/internal/protocol"
)

func TestDefaultTransportModeIsOpen(t *testing.T) {
	server := &Server{}
	if got := server.TransportMode(); got != protocol.TransportOpen {
		t.Fatalf("default transport mode = %q, want %q", got, protocol.TransportOpen)
	}
}
