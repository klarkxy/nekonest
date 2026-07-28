package adapters

import (
	"fmt"
	"strings"
)

// Registry owns the enabled adapter set and provides deterministic lookup.
type Registry struct {
	ordered []Adapter
	byName  map[string]Adapter
}

// NewRegistry creates a registry and rejects empty or duplicate adapter names.
func NewRegistry(list ...Adapter) (*Registry, error) {
	r := &Registry{
		ordered: make([]Adapter, 0, len(list)),
		byName:  make(map[string]Adapter, len(list)),
	}
	for _, adapter := range list {
		if adapter == nil {
			return nil, fmt.Errorf("register nil adapter")
		}
		name := strings.TrimSpace(adapter.Name())
		if name == "" {
			return nil, fmt.Errorf("register adapter with empty name")
		}
		if _, exists := r.byName[name]; exists {
			return nil, fmt.Errorf("duplicate adapter name %q", name)
		}
		r.byName[name] = adapter
		r.ordered = append(r.ordered, adapter)
	}
	return r, nil
}

// NewDefaultRegistry returns all built-in adapters in stable display order.
func NewDefaultRegistry() (*Registry, error) {
	return NewRegistry(
		NewClaudeCodeAdapter(),
		NewCodexAdapter(),
		NewKiloAdapter(),
		NewKimiCLIAdapter(),
		NewGrokBuildAdapter(),
	)
}

// All returns a copy of the ordered adapter slice.
func (r *Registry) All() []Adapter {
	if r == nil {
		return nil
	}
	return append([]Adapter(nil), r.ordered...)
}

// Get resolves an adapter by its public agent type.
func (r *Registry) Get(name string) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	adapter, ok := r.byName[name]
	return adapter, ok
}

// SetOutputSink installs one normalized sink on every streaming adapter.
func (r *Registry) SetOutputSink(sink OutputSink) {
	if r == nil {
		return
	}
	for _, adapter := range r.ordered {
		if source, ok := adapter.(OutputAdapter); ok {
			source.SetOutputSink(sink)
		}
	}
}

// Close releases resources held by every adapter. All adapters are attempted.
func (r *Registry) Close() error {
	if r == nil {
		return nil
	}
	var errs []string
	for _, adapter := range r.ordered {
		closer, ok := adapter.(ClosableAdapter)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", adapter.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close adapters: %s", strings.Join(errs, "; "))
	}
	return nil
}
