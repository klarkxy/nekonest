package adapters

import (
	"testing"
	"time"
)

func assertWatchClosed(t *testing.T, watch <-chan *SessionInfo) {
	t.Helper()
	select {
	case _, ok := <-watch:
		if ok {
			t.Fatal("watch emitted instead of closing")
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not close")
	}
}
