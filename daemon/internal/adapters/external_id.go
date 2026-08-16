package adapters

import (
	"fmt"
	"strings"
)

func publicSessionID(agent AgentType, nativeID string) string {
	nativeID = strings.TrimSpace(nativeID)
	if nativeID == "" {
		return ""
	}
	prefix := string(agent) + ":"
	if strings.HasPrefix(nativeID, prefix) {
		return nativeID
	}
	return prefix + nativeID
}

func nativeSessionID(agent AgentType, publicID string) (string, error) {
	prefix := string(agent) + ":"
	if !strings.HasPrefix(publicID, prefix) {
		return "", fmt.Errorf("session %q does not belong to %s", publicID, agent)
	}
	nativeID := strings.TrimSpace(strings.TrimPrefix(publicID, prefix))
	if nativeID == "" {
		return "", fmt.Errorf("empty native session id for %s", agent)
	}
	return nativeID, nil
}
