// Package events provides the concurrency-safe runtime event bus.
package events

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/WhoIsTheAxl/Zicor/api"
)

var (
	// ErrClosed is returned when an operation is attempted after Bus.Close.
	ErrClosed = errors.New("event bus is closed")
	// ErrNilContext is returned when Publish receives a nil context.
	ErrNilContext = errors.New("event context is nil")
	// ErrNilEvent is returned when Publish receives a nil event.
	ErrNilEvent = errors.New("event is nil")
	// ErrNilEventType is returned when Subscribe receives no event type.
	ErrNilEventType = errors.New("event type is nil")
	// ErrNilHandler is returned when Subscribe receives no handler.
	ErrNilHandler = errors.New("event handler is nil")
	// ErrInterfaceEventType is returned when Subscribe attempts to subscribe
	// to an interface rather than an exact concrete event type.
	ErrInterfaceEventType = errors.New("event type must be concrete, not an interface")
)

// Bus dispatches events to subscribers of the exact concrete Go event type.
// It invokes handlers synchronously and in subscription order. A handler
// error or panic is reported to the publisher, but does not prevent the
// remaining handlers from running. The zero value is ready for use.
type Bus struct {
	mu     sync.RWMutex
	closed bool
	nextID uint64
	subs   map[reflect.Type][]*subscription
}

// New constructs an empty event bus.
func New() *Bus {
	return &Bus{subs: make(map[reflect.Type][]*subscription)}
}

// Subscribe registers handler for the exact eventType. The returned
// subscription can be cancelled concurrently and repeatedly.
func (b *Bus) Subscribe(eventType reflect.Type, handler api.EventHandler) (api.Subscription, error) {
	if b == nil {
		return nil, errors.New("event bus is nil")
	}
	if eventType == nil {
		return nil, ErrNilEventType
	}
	if eventType.Kind() == reflect.Interface {
		return nil, ErrInterfaceEventType
	}
	if handler == nil {
		return nil, ErrNilHandler
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrClosed
	}
	if b.subs == nil {
		b.subs = make(map[reflect.Type][]*subscription)
	}
	b.nextID++
	sub := &subscription{
		bus:       b,
		eventType: eventType,
		id:        b.nextID,
		handler:   handler,
	}
	b.subs[eventType] = append(b.subs[eventType], sub)
	return sub, nil
}

// Publish delivers event to a snapshot of subscribers registered for its
// exact concrete type. Handler errors are joined and returned after all
// eligible handlers have run. The context is checked before each handler.
func (b *Bus) Publish(ctx context.Context, event any) error {
	if b == nil {
		return errors.New("event bus is nil")
	}
	if ctx == nil {
		return ErrNilContext
	}
	if isNilEvent(event) {
		return ErrNilEvent
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	eventType := reflect.TypeOf(event)
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrClosed
	}
	current := append([]*subscription(nil), b.subs[eventType]...)
	b.mu.RUnlock()

	var errs []error
	for _, sub := range current {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if sub.cancelled.Load() {
			continue
		}
		if err := invoke(ctx, sub.handler, event); err != nil {
			errs = append(errs, fmt.Errorf("event handler #%d for %s: %w", sub.id, eventType, err))
		}
	}
	return errors.Join(errs...)
}

// Close prevents new subscriptions and publications and cancels all current
// subscriptions. A handler already executing may finish.
func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	for _, list := range b.subs {
		for _, sub := range list {
			sub.cancelled.Store(true)
		}
	}
	b.subs = make(map[reflect.Type][]*subscription)
	b.mu.Unlock()
}

func (b *Bus) remove(sub *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := b.subs[sub.eventType]
	for i, candidate := range list {
		if candidate.id == sub.id {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(b.subs, sub.eventType)
	} else {
		b.subs[sub.eventType] = list
	}
}

type subscription struct {
	bus       *Bus
	eventType reflect.Type
	id        uint64
	handler   api.EventHandler
	cancelled atomic.Bool
}

// Cancel unregisters the subscription. It is safe to call more than once.
func (s *subscription) Cancel() {
	if s == nil || !s.cancelled.CompareAndSwap(false, true) {
		return
	}
	s.bus.remove(s)
}

func isNilEvent(event any) bool {
	if event == nil {
		return true
	}
	value := reflect.ValueOf(event)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func invoke(ctx context.Context, handler api.EventHandler, event any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("handler panicked: %v\n%s", recovered, debug.Stack())
		}
	}()
	return handler(ctx, event)
}
