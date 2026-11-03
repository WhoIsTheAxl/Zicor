package events

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/WhoIsTheAxl/Zicor/api"
)

type testEvent struct {
	Value int
}

func TestTypedSubscribePublishAndCancel(t *testing.T) {
	bus := New()
	defer bus.Close()

	var got []int
	sub, err := api.Subscribe[testEvent](bus, func(_ context.Context, event testEvent) error {
		got = append(got, event.Value)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if err := api.Publish(context.Background(), bus, testEvent{Value: 7}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	sub.Cancel()
	sub.Cancel()
	if err := api.Publish(context.Background(), bus, testEvent{Value: 9}); err != nil {
		t.Fatalf("Publish after cancellation error = %v", err)
	}
	if !reflect.DeepEqual(got, []int{7}) {
		t.Fatalf("received values = %v, want [7]", got)
	}
}

func TestPublishUsesExactEventType(t *testing.T) {
	bus := New()
	defer bus.Close()

	var calls int
	if _, err := api.Subscribe[testEvent](bus, func(context.Context, testEvent) error {
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), &testEvent{Value: 1}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("pointer event unexpectedly invoked value subscriber %d times", calls)
	}
}

func TestPublishContinuesAfterHandlerErrorsAndPanics(t *testing.T) {
	bus := New()
	defer bus.Close()

	expected := errors.New("subscriber failed")
	var called []string
	_, err := api.Subscribe[testEvent](bus, func(context.Context, testEvent) error {
		called = append(called, "error")
		return expected
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.Subscribe[testEvent](bus, func(context.Context, testEvent) error {
		called = append(called, "panic")
		panic("broken subscriber")
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.Subscribe[testEvent](bus, func(context.Context, testEvent) error {
		called = append(called, "last")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = api.Publish(context.Background(), bus, testEvent{})
	if !errors.Is(err, expected) {
		t.Fatalf("Publish() error = %v, want wrapped subscriber error", err)
	}
	if !strings.Contains(err.Error(), "handler panicked") {
		t.Fatalf("Publish() error does not include recovered panic: %v", err)
	}
	if !reflect.DeepEqual(called, []string{"error", "panic", "last"}) {
		t.Fatalf("handler order = %v", called)
	}
}

func TestPublishHonoursCancelledContext(t *testing.T) {
	bus := New()
	defer bus.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	_, err := api.Subscribe[testEvent](bus, func(context.Context, testEvent) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(ctx, testEvent{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish() error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("handler was called with an already-cancelled context")
	}
}

func TestCloseIsIdempotentAndRejectsOperations(t *testing.T) {
	bus := New()
	sub, err := api.Subscribe[testEvent](bus, func(context.Context, testEvent) error {
		t.Fatal("closed bus invoked handler")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	bus.Close()
	bus.Close()
	sub.Cancel()

	if _, err := api.Subscribe[testEvent](bus, func(context.Context, testEvent) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe() error = %v, want ErrClosed", err)
	}
	if err := bus.Publish(context.Background(), testEvent{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Publish() error = %v, want ErrClosed", err)
	}
}

func TestSubscribeRejectsInvalidArguments(t *testing.T) {
	bus := New()
	defer bus.Close()

	if _, err := bus.Subscribe(nil, func(context.Context, any) error { return nil }); !errors.Is(err, ErrNilEventType) {
		t.Fatalf("nil event type error = %v", err)
	}
	if _, err := bus.Subscribe(reflect.TypeFor[any](), func(context.Context, any) error { return nil }); !errors.Is(err, ErrInterfaceEventType) {
		t.Fatalf("interface event type error = %v", err)
	}
	if _, err := bus.Subscribe(reflect.TypeFor[testEvent](), nil); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("nil handler error = %v", err)
	}
	if _, err := api.Subscribe[testEvent](nil, func(context.Context, testEvent) error { return nil }); err == nil {
		t.Fatal("expected nil bus error")
	}
	if _, err := api.Subscribe[testEvent](bus, nil); err == nil {
		t.Fatal("expected nil typed handler error")
	}
}

func TestPublishRejectsNilArguments(t *testing.T) {
	bus := New()
	defer bus.Close()

	if err := bus.Publish(nil, testEvent{}); !errors.Is(err, ErrNilContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := bus.Publish(context.Background(), nil); !errors.Is(err, ErrNilEvent) {
		t.Fatalf("nil event error = %v", err)
	}
	var event *testEvent
	if err := bus.Publish(context.Background(), event); !errors.Is(err, ErrNilEvent) {
		t.Fatalf("typed nil event error = %v", err)
	}
}

func TestConcurrentSubscribeCancelAndPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	const workers = 24
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub, err := api.Subscribe[testEvent](bus, func(context.Context, testEvent) error { return nil })
			if err != nil {
				t.Errorf("Subscribe() error = %v", err)
				return
			}
			_ = bus.Publish(context.Background(), testEvent{})
			sub.Cancel()
		}()
	}
	wg.Wait()
}

func TestZeroValueBus(t *testing.T) {
	var bus Bus
	called := false
	_, err := api.Subscribe[testEvent](&bus, func(context.Context, testEvent) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), testEvent{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("zero-value bus did not dispatch event")
	}
	bus.Close()
}
