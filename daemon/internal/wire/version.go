// Package wire owns daemon-side constants for the NekoNest wire protocol.
package wire

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	CurrentProtocolVersion = "1.2"
	CurrentProtocolMajor   = 1
	CurrentProtocolMinor   = 2
)

// ValidateNegotiatedVersion accepts only a version this daemon actually
// advertised support for. It prevents a malformed auth response from silently
// changing later frame/AAD versioning.
func ValidateNegotiatedVersion(value string) error {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return fmt.Errorf("protocol_version must be major.minor")
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid protocol major")
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return fmt.Errorf("invalid protocol minor")
	}
	if major != CurrentProtocolMajor || minor > CurrentProtocolMinor {
		return fmt.Errorf("unsupported negotiated protocol_version %q", value)
	}
	return nil
}
