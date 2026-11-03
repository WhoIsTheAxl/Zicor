package mods

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WhoIsTheAxl/Zicor/api"
)

type sessionTestEvent struct {
	Text string
}

type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	l.mu.Lock()
	l.calls = append(l.calls, call)
	l.mu.Unlock()
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

type sessionTestMod struct {
	name     string
	log      *callLog
	init     func(api.ModContext) error
	shutdown func(context.Context) error
}

func (m *sessionTestMod) Init(ctx api.ModContext) error {
	m.log.add("init:" + m.name)
	if m.init != nil {
		return m.init(ctx)
	}
	return nil
}

func (m *sessionTestMod) Shutdown(ctx context.Context) error {
	m.log.add("shutdown:" + m.name)
	if m.shutdown != nil {
		return m.shutdown(ctx)
	}
	return nil
}

func sessionMod(id string, deps ...api.Dependency) DiscoveredMod {
	return DiscoveredMod{
		Directory: id,
		Manifest: api.Manifest{
			ID:           id,
			Name:         id,
			Version:      "1.0.0",
			APIVersion:   api.CurrentAPIVersion,
			Dependencies: deps,
		},
	}
}

func registerSessionMod(t *testing.T, registry *Registry, id string, log *callLog, init func(api.ModContext) error, shutdown func(context.Context) error) {
	t.Helper()
	err := registry.Register(id, func() api.Mod {
		return &sessionTestMod{name: id, log: log, init: init, shutdown: shutdown}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStartSessionUsesDependencyOrderAndClosesInReverse(t *testing.T) {
	registry := NewRegistry()
	log := &callLog{}
	var modContext api.ModContext

	registerSessionMod(t, registry, "core", log, nil, func(cleanup context.Context) error {
		if modContext.Context().Err() != context.Canceled {
			return fmt.Errorf("mod context was not cancelled before Shutdown: %v", modContext.Context().Err())
		}
		if _, ok := cleanup.Deadline(); !ok {
			return errors.New("cleanup context has no deadline")
		}
		return nil
	})
	registerSessionMod(t, registry, "app", log, func(ctx api.ModContext) error {
		modContext = ctx
		return nil
	}, nil)

	session, err := StartSession(context.Background(), SessionOptions{
		Registry: registry,
		Mods: []DiscoveredMod{
			sessionMod("app", api.Dependency{ID: "core", Version: "1.0.0"}),
			sessionMod("core"),
		},
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	if got, want := log.snapshot(), []string{"init:core", "init:app"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("startup order = %v, want %v", got, want)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	want := []string{"init:core", "init:app", "shutdown:app", "shutdown:core"}
	if got := log.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("lifecycle calls = %v, want %v", got, want)
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("Done() was not closed after shutdown")
	}
}

func TestStartSessionRollsBackWhenInitializationFails(t *testing.T) {
	registry := NewRegistry()
	log := &callLog{}
	expected := errors.New("application initialization failed")

	registerSessionMod(t, registry, "core", log, nil, nil)
	registerSessionMod(t, registry, "app", log, func(api.ModContext) error {
		return expected
	}, nil)

	session, err := StartSession(context.Background(), SessionOptions{
		Registry: registry,
		Mods: []DiscoveredMod{
			sessionMod("app", api.Dependency{ID: "core"}),
			sessionMod("core"),
		},
	})
	if session != nil {
		t.Fatal("StartSession() returned a session after failure")
	}
	if !errors.Is(err, expected) {
		t.Fatalf("StartSession() error = %v, want wrapped initialization error", err)
	}
	want := []string{"init:core", "init:app", "shutdown:core"}
	if got := log.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback calls = %v, want %v", got, want)
	}
}

func TestStartSessionRecoversInitializationPanic(t *testing.T) {
	registry := NewRegistry()
	log := &callLog{}
	registerSessionMod(t, registry, "panicmod", log, func(api.ModContext) error {
		panic("mod init exploded")
	}, nil)

	_, err := StartSession(context.Background(), SessionOptions{
		Registry: registry,
		Mods:     []DiscoveredMod{sessionMod("panicmod")},
	})
	if err == nil || !strings.Contains(err.Error(), "mod init exploded") {
		t.Fatalf("StartSession() error = %v, want recovered panic", err)
	}
	if got, want := log.snapshot(), []string{"init:panicmod"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lifecycle calls = %v, want %v", got, want)
	}
}

func TestStartSessionRequiresFactoriesBeforeInitializingAnyMod(t *testing.T) {
	registry := NewRegistry()
	log := &callLog{}
	registerSessionMod(t, registry, "present", log, nil, nil)

	_, err := StartSession(context.Background(), SessionOptions{
		Registry: registry,
		Mods: []DiscoveredMod{
			sessionMod("present"),
			sessionMod("missing"),
		},
	})
	if !errors.Is(err, ErrModNotRegistered) {
		t.Fatalf("StartSession() error = %v, want ErrModNotRegistered", err)
	}
	if calls := log.snapshot(); len(calls) != 0 {
		t.Fatalf("mods initialized before registry validation: %v", calls)
	}
}

func TestSessionEventsAndSubscriptionsFollowModContext(t *testing.T) {
	registry := NewRegistry()
	log := &callLog{}
	var modContext api.ModContext
	var received int
	registerSessionMod(t, registry, "listener", log, func(ctx api.ModContext) error {
		modContext = ctx
		_, err := api.Subscribe[sessionTestEvent](ctx.Events(), func(context.Context, sessionTestEvent) error {
			received++
			return nil
		})
		return err
	}, nil)

	parent, cancel := context.WithCancel(context.Background())
	session, err := StartSession(parent, SessionOptions{
		Registry: registry,
		Mods:     []DiscoveredMod{sessionMod("listener")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Publish(context.Background(), session.Events(), sessionTestEvent{Text: "before"}); err != nil {
		t.Fatal(err)
	}
	if received != 1 {
		t.Fatalf("received %d events before shutdown, want 1", received)
	}

	cancel()
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session did not stop after parent cancellation")
	}
	if modContext.Context().Err() != context.Canceled {
		t.Fatalf("mod context error = %v, want context.Canceled", modContext.Context().Err())
	}
	if _, err := api.Subscribe[sessionTestEvent](modContext.Events(), func(context.Context, sessionTestEvent) error {
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("subscription after context cancellation error = %v", err)
	}
	if err := api.Publish(context.Background(), session.Events(), sessionTestEvent{}); err == nil {
		t.Fatal("publish succeeded after session event bus closed")
	}
	if received != 1 {
		t.Fatalf("received %d events after shutdown, want 1", received)
	}
}

func TestSessionCloseCollectsShutdownErrorsAndContinues(t *testing.T) {
	registry := NewRegistry()
	log := &callLog{}
	expected := errors.New("shutdown failed")
	registerSessionMod(t, registry, "core", log, nil, nil)
	registerSessionMod(t, registry, "app", log, nil, func(context.Context) error {
		return expected
	})

	session, err := StartSession(context.Background(), SessionOptions{
		Registry: registry,
		Mods: []DiscoveredMod{
			sessionMod("app", api.Dependency{ID: "core"}),
			sessionMod("core"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); !errors.Is(err, expected) {
		t.Fatalf("Close() error = %v, want joined shutdown error", err)
	}
	want := []string{"init:core", "init:app", "shutdown:app", "shutdown:core"}
	if got := log.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("shutdown stopped early: got %v, want %v", got, want)
	}
}

func TestSessionShutdownWaitIsBounded(t *testing.T) {
	registry := NewRegistry()
	log := &callLog{}
	registerSessionMod(t, registry, "slow", log, nil, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	session, err := StartSession(context.Background(), SessionOptions{
		Registry:        registry,
		Mods:            []DiscoveredMod{sessionMod("slow")},
		ShutdownTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = session.Close()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Close() took %v despite the configured timeout", elapsed)
	}
}
