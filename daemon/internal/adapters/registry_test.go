package adapters

import (
	"fmt"
	"testing"
)

type registryTestAdapter struct {
	name string
	sink OutputSink
}

func (a *registryTestAdapter) Name() string                      { return a.name }
func (a *registryTestAdapter) IsAvailable() bool                 { return true }
func (a *registryTestAdapter) Discover() ([]*SessionInfo, error) { return nil, nil }
func (a *registryTestAdapter) OwnsSession(string) bool           { return false }
func (a *registryTestAdapter) Watch(string) (<-chan *SessionInfo, error) {
	return nil, fmt.Errorf("unused")
}
func (a *registryTestAdapter) SendPrompt(string, string) error { return nil }
func (a *registryTestAdapter) Approve(string, string) error    { return nil }
func (a *registryTestAdapter) Deny(string, string) error       { return nil }
func (a *registryTestAdapter) Interrupt(string) error          { return nil }
func (a *registryTestAdapter) FetchHistory(string, int) ([]*HistoryMessage, error) {
	return nil, nil
}
func (a *registryTestAdapter) SetOutputSink(sink OutputSink) { a.sink = sink }

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	_, err := NewRegistry(
		&registryTestAdapter{name: "same"},
		&registryTestAdapter{name: "same"},
	)
	if err == nil {
		t.Fatal("NewRegistry accepted duplicate adapter names")
	}
}

func TestRegistryLookupOrderAndOutputSink(t *testing.T) {
	first := &registryTestAdapter{name: "first"}
	second := &registryTestAdapter{name: "second"}
	registry, err := NewRegistry(first, second)
	if err != nil {
		t.Fatal(err)
	}
	got := registry.All()
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("All() = %#v", got)
	}
	if found, ok := registry.Get("second"); !ok || found != second {
		t.Fatalf("Get(second) = %#v, %v", found, ok)
	}

	var event OutputEvent
	registry.SetOutputSink(func(got OutputEvent) { event = got })
	first.sink(OutputEvent{SessionID: "s", AgentType: "first", Content: "ok"})
	if event.SessionID != "s" || event.Content != "ok" {
		t.Fatalf("sink event = %#v", event)
	}
}
