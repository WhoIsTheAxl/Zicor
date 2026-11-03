package mods

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/WhoIsTheAxl/Zicor/api"
)

type registryTestMod struct{}

func (registryTestMod) Init(api.ModContext) error { return nil }
func (registryTestMod) Shutdown(context.Context) error { return nil }

type nilRegistryTestMod struct{}

func (*nilRegistryTestMod) Init(api.ModContext) error { return nil }
func (*nilRegistryTestMod) Shutdown(context.Context) error { return nil }

func TestRegistryRegisterAndCreate(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("example", func() api.Mod { return registryTestMod{} }); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if !registry.Has("example") {
		t.Fatal("registered mod not found")
	}
	mod, err := registry.New("example")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := mod.(registryTestMod); !ok {
		t.Fatalf("New() returned %T", mod)
	}
}

func TestRegistryRejectsInvalidAndDuplicateRegistrations(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("../escape", func() api.Mod { return registryTestMod{} }); err == nil {
		t.Fatal("expected invalid ID error")
	}
	if err := registry.Register("example", nil); !errors.Is(err, ErrNilModFactory) {
		t.Fatalf("nil factory error = %v", err)
	}
	factory := api.ModFactory(func() api.Mod { return registryTestMod{} })
	if err := registry.Register("example", factory); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("example", factory); !errors.Is(err, ErrModAlreadyRegistered) {
		t.Fatalf("duplicate registration error = %v", err)
	}
}

func TestRegistryNewRejectsMissingAndNilInstances(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.New("missing"); !errors.Is(err, ErrModNotRegistered) {
		t.Fatalf("missing mod error = %v", err)
	}

	var typedNil *nilRegistryTestMod
	if err := registry.Register("nilmod", func() api.Mod { return typedNil }); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.New("nilmod"); !errors.Is(err, ErrNilModInstance) {
		t.Fatalf("typed nil instance error = %v", err)
	}
}

func TestRegistryNewConvertsFactoryPanicToError(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("panicmod", func() api.Mod { panic("factory failure") }); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.New("panicmod"); err == nil || !strings.Contains(err.Error(), "factory failure") {
		t.Fatalf("New() error = %v, want panic details", err)
	}
}

func TestRegistryIDsAreSortedAndIndependent(t *testing.T) {
	registry := NewRegistry()
	for _, id := range []string{"zeta", "alpha", "middle"} {
		if err := registry.Register(id, func() api.Mod { return registryTestMod{} }); err != nil {
			t.Fatal(err)
		}
	}
	ids := registry.IDs()
	if !reflect.DeepEqual(ids, []string{"alpha", "middle", "zeta"}) {
		t.Fatalf("IDs() = %v", ids)
	}
	ids[0] = "modified"
	if registry.IDs()[0] != "alpha" {
		t.Fatal("IDs() exposed internal registry state")
	}
}

func TestRegistryConcurrentRegistrationAndLookup(t *testing.T) {
	registry := NewRegistry()
	const count = 32
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("mod_%02d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := registry.Register(id, func() api.Mod { return registryTestMod{} }); err != nil {
				t.Errorf("Register(%q) error = %v", id, err)
				return
			}
			if _, err := registry.New(id); err != nil {
				t.Errorf("New(%q) error = %v", id, err)
			}
		}()
	}
	wg.Wait()
}

func TestZeroValueRegistry(t *testing.T) {
	var registry Registry
	if err := registry.Register("zeromod", func() api.Mod { return registryTestMod{} }); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.New("zeromod"); err != nil {
		t.Fatal(err)
	}
}
