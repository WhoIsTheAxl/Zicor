// Package api defines event contracts exposed to Zicor mods.
package api

import (
	"context"
	"errors"
	"reflect"
)

// EventHandler handles one event. Returning an error does not prevent other
// subscribers from receiving the same event; the publisher receives the
// combined errors after dispatch completes.
type EventHandler func(context.Context, any) error

// Subscription represents one event subscription. Cancel is safe to call
// multiple times. A handler already executing when Cancel is called may finish.
type Subscription interface {
	Cancel()
}

// EventBus publishes values to subscribers registered for the exact concrete
// Go type of the event. Implementations must be safe for concurrent use.
type EventBus interface {
	Subscribe(eventType reflect.Type, handler EventHandler) (Subscription, error)
	Publish(ctx context.Context, event any) error
}

// Subscribe registers a type-safe event handler.
//
// Event types should be concrete structs or pointers to structs. The event
// bus matches the exact event type; it does not perform assignability-based
// matching or deliver an event to interfaces implemented by its type.
func Subscribe[T any](bus EventBus, handler func(context.Context, T) error) (Subscription, error) {
	if bus == nil {
		return nil, errors.New("event bus is nil")
	}
	if handler == nil {
		return nil, errors.New("event handler is nil")
	}

	eventType := reflect.TypeFor[T]()
	return bus.Subscribe(eventType, func(ctx context.Context, event any) error {
		typed, ok := event.(T)
		if !ok {
			return errors.New("event bus delivered an unexpected event type")
		}
		return handler(ctx, typed)
	})
}

// Publish publishes an event through a mod's event bus.
func Publish[T any](ctx context.Context, bus EventBus, event T) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	return bus.Publish(ctx, event)
}
