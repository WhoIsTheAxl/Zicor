// Package mods discovers, validates, and registers Zicor mods.
package mods

import (
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"sort"
	"sync"

	"github.com/WhoIsTheAxl/Zicor/api"
)

var (
	ErrModAlreadyRegistered = errors.New("mod is already registered")
	ErrModNotRegistered     = errors.New("mod is not registered")
	ErrNilModFactory        = errors.New("mod factory is nil")
	ErrNilModInstance       = errors.New("mod factory returned a nil instance")
)

// Registry maps validated mod IDs to factories compiled into the Zicor
// executable. It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]api.ModFactory
}

// NewRegistry creates an empty static mod registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]api.ModFactory)}
}

// Register associates id with a factory. A mod ID can be registered only
// once. Registration is intended to be performed during application setup.
func (r *Registry) Register(id string, factory api.ModFactory) error {
	if r == nil {
		return errors.New("mod registry is nil")
	}
	if err := api.ValidateModID(id); err != nil {
		return err
	}
	if factory == nil {
		return ErrNilModFactory
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[id]; exists {
		return fmt.Errorf("%w: %q", ErrModAlreadyRegistered, id)
	}
	r.factories[id] = factory
	return nil
}

// Has reports whether id has a compiled factory registered.
func (r *Registry) Has(id string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[id]
	return ok
}

// IDs returns all registered mod IDs in lexical order.
func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	ids := make([]string, 0, len(r.factories))
	for id := range r.factories {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	sort.Strings(ids)
	return ids
}

// New creates a fresh instance using the factory registered for id. Factory
// execution occurs outside the registry lock so factories cannot block other
// registry operations. Panics are converted to errors with a stack trace.
func (r *Registry) New(id string) (mod api.Mod, err error) {
	if r == nil {
		return nil, errors.New("mod registry is nil")
	}

	r.mu.RLock()
	factory, ok := r.factories[id]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrModNotRegistered, id)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			mod = nil
			err = fmt.Errorf("mod factory for %q panicked: %v\n%s", id, recovered, debug.Stack())
		}
	}()
	mod = factory()
	if isNilMod(mod) {
		return nil, fmt.Errorf("%w: %q", ErrNilModInstance, id)
	}
	return mod, nil
}

func isNilMod(mod api.Mod) bool {
	if mod == nil {
		return true
	}
	value := reflect.ValueOf(mod)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
