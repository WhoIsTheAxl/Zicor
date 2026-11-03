package mods

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	"github.com/WhoIsTheAxl/Zicor/api"
	"github.com/WhoIsTheAxl/Zicor/api/minecraft"
	"github.com/WhoIsTheAxl/Zicor/internal/events"
)

const defaultShutdownTimeout = 5 * time.Second

// SessionOptions configures one connected Minecraft client's mod session.
type SessionOptions struct {
	// Registry contains factories for the Go mods compiled into the host.
	Registry *Registry

	// Mods is the set of discovered manifests to load. Dependencies are
	// validated and mods are initialized in dependency-first order.
	Mods []DiscoveredMod

	// Client is the Minecraft client capability shared with all mods.
	Client minecraft.Client

	// Logger is the base structured logger. Each mod receives a logger with
	// its mod ID attached. nil uses slog.Default().
	Logger *slog.Logger

	// ShutdownTimeout bounds how long the session waits for each mod's
	// Shutdown method. Values <= 0 use the default timeout.
	ShutdownTimeout time.Duration
}

type activeMod struct {
	id     string
	mod    api.Mod
	cancel context.CancelFunc
}

// Session owns the initialized mod instances and the event bus for one
// connection. It shuts mods down in reverse dependency order and is safe for
// concurrent Close calls.
type Session struct {
	mu              sync.Mutex
	mods            []activeMod
	eventBus        *events.Bus
	shutdownTimeout time.Duration
	done            chan struct{}
	closing         bool
	closed          bool
	closeErr        error
}

// StartSession validates the selected mods, verifies that each has a
// statically registered factory, initializes them in dependency order, and
// rolls back already initialized mods if startup fails.
//
// The parent context controls the lifetime of the session. When it is
// cancelled, the session automatically begins shutdown.
func StartSession(parent context.Context, options SessionOptions) (*Session, error) {
	if parent == nil {
		return nil, errors.New("session parent context is nil")
	}
	if err := parent.Err(); err != nil {
		return nil, fmt.Errorf("session context is already done: %w", err)
	}
	if options.Registry == nil {
		return nil, errors.New("mod registry is nil")
	}

	ordered, err := ResolveOrder(options.Mods)
	if err != nil {
		return nil, fmt.Errorf("resolve mod dependencies: %w", err)
	}
	for _, mod := range ordered {
		if !options.Registry.Has(mod.Manifest.ID) {
			return nil, fmt.Errorf("%w: %q", ErrModNotRegistered, mod.Manifest.ID)
		}
	}

	timeout := options.ShutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	session := &Session{
		eventBus:        events.New(),
		shutdownTimeout: timeout,
		done:            make(chan struct{}),
	}

	for _, discovered := range ordered {
		if err := parent.Err(); err != nil {
			rollbackErr := session.Close()
			return nil, errors.Join(
				fmt.Errorf("session cancelled while initializing mods: %w", err),
				wrapRollbackError(rollbackErr),
			)
		}

		id := discovered.Manifest.ID
		instance, err := options.Registry.New(id)
		if err != nil {
			rollbackErr := session.Close()
			return nil, errors.Join(
				fmt.Errorf("construct mod %q: %w", id, err),
				wrapRollbackError(rollbackErr),
			)
		}

		modCtx, cancel := context.WithCancel(parent)
		info := api.ModInfo{
			ID:         discovered.Manifest.ID,
			Name:       discovered.Manifest.Name,
			Version:    discovered.Manifest.Version,
			APIVersion: discovered.Manifest.APIVersion,
		}
		ctx := newModContext(modCtx, info, options.Client, logger.With("mod", id), session.eventBus)
		if err := initializeMod(instance, ctx); err != nil {
			cancel()
			rollbackErr := session.Close()
			return nil, errors.Join(
				fmt.Errorf("initialize mod %q: %w", id, err),
				wrapRollbackError(rollbackErr),
			)
		}

		session.mods = append(session.mods, activeMod{
			id:     id,
			mod:    instance,
			cancel: cancel,
		})
	}

	if err := parent.Err(); err != nil {
		rollbackErr := session.Close()
		return nil, errors.Join(
			fmt.Errorf("session context ended during startup: %w", err),
			wrapRollbackError(rollbackErr),
		)
	}

	go func() {
		select {
		case <-parent.Done():
			_ = session.Close()
		case <-session.done:
		}
	}()
	return session, nil
}

// Events returns the event bus shared by the mods in this session. It is
// closed when the session finishes shutting down.
func (s *Session) Events() api.EventBus {
	if s == nil {
		return nil
	}
	return s.eventBus
}

// Done is closed after all mod shutdown attempts have completed.
func (s *Session) Done() <-chan struct{} {
	if s == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return s.done
}

// Close cancels and shuts down initialized mods in reverse dependency order.
// Each Shutdown call receives a fresh context with a bounded deadline.
// Multiple concurrent calls wait for the same shutdown operation.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	if s.closing {
		done := s.done
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	s.closing = true
	mods := append([]activeMod(nil), s.mods...)
	s.mu.Unlock()

	var shutdownErrors []error
	for _, entry := range mods {
		entry.cancel()
	}
	for i := len(mods) - 1; i >= 0; i-- {
		entry := mods[i]

		cleanupCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		result := make(chan error, 1)
		go func(mod api.Mod) {
			result <- shutdownMod(mod, cleanupCtx)
		}(entry.mod)

		select {
		case err := <-result:
			if err != nil {
				shutdownErrors = append(shutdownErrors, fmt.Errorf("shutdown mod %q: %w", entry.id, err))
			}
		case <-cleanupCtx.Done():
			shutdownErrors = append(shutdownErrors, fmt.Errorf("shutdown mod %q timed out: %w", entry.id, cleanupCtx.Err()))
		}
		cancel()
	}

	s.eventBus.Close()
	s.mu.Lock()
	s.closeErr = errors.Join(shutdownErrors...)
	s.closed = true
	close(s.done)
	err := s.closeErr
	s.mu.Unlock()
	return err
}

func wrapRollbackError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("mod startup rollback: %w", err)
}

func initializeMod(mod api.Mod, ctx api.ModContext) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Init panicked: %v\n%s", recovered, debug.Stack())
		}
	}()
	return mod.Init(ctx)
}

func shutdownMod(mod api.Mod, ctx context.Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Shutdown panicked: %v\n%s", recovered, debug.Stack())
		}
	}()
	return mod.Shutdown(ctx)
}

type modContext struct {
	ctx    context.Context
	client minecraft.Client
	events api.EventBus
	logger api.Logger
	info   api.ModInfo
}

func newModContext(
	ctx context.Context,
	info api.ModInfo,
	client minecraft.Client,
	logger *slog.Logger,
	eventBus api.EventBus,
) api.ModContext {
	return &modContext{
		ctx:    ctx,
		client: client,
		events: scopedEventBus{ctx: ctx, bus: eventBus},
		logger: slogModLogger{logger: logger},
		info:   info,
	}
}

func (c *modContext) Context() context.Context { return c.ctx }
func (c *modContext) Client() minecraft.Client { return c.client }
func (c *modContext) Events() api.EventBus     { return c.events }
func (c *modContext) Logger() api.Logger        { return c.logger }
func (c *modContext) Info() api.ModInfo         { return c.info }

type slogModLogger struct {
	logger *slog.Logger
}

func (l slogModLogger) Info(message string, args ...any) {
	l.logger.Info(message, args...)
}

func (l slogModLogger) Warn(message string, args ...any) {
	l.logger.Warn(message, args...)
}

func (l slogModLogger) Error(message string, args ...any) {
	l.logger.Error(message, args...)
}

type scopedEventBus struct {
	ctx context.Context
	bus api.EventBus
}

func (b scopedEventBus) Subscribe(eventType reflect.Type, handler api.EventHandler) (api.Subscription, error) {
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, errors.New("event handler is nil")
	}
	sub, err := b.bus.Subscribe(eventType, func(eventCtx context.Context, event any) error {
		if b.ctx.Err() != nil {
			return nil
		}
		handlerCtx, cancel := context.WithCancel(eventCtx)
		stop := context.AfterFunc(b.ctx, cancel)
		defer func() {
			stop()
			cancel()
		}()
		if handlerCtx.Err() != nil {
			return nil
		}
		return handler(handlerCtx, event)
	})
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(b.ctx, sub.Cancel)
	if err := b.ctx.Err(); err != nil {
		stop()
		sub.Cancel()
		return nil, err
	}
	return &scopedSubscription{Subscription: sub, stop: stop}, nil
}

func (b scopedEventBus) Publish(ctx context.Context, event any) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	return b.bus.Publish(ctx, event)
}

type scopedSubscription struct {
	api.Subscription
	stop func() bool
}

func (s *scopedSubscription) Cancel() {
	s.stop()
	s.Subscription.Cancel()
}
