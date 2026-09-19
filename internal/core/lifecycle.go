package core

type LifecycleState int

const (
    StateCreated LifecycleState = iota
    StateStarting
    StateRunning
    StateStopping
    StateStopped
)

type Lifecycle struct {
    state LifecycleState
}

func NewLifecycle() *Lifecycle {
    return &Lifecycle{
        state: StateCreated,
    }
}

func (l *Lifecycle) Start() error {
    l.state = StateRunning
    return nil
}

func (l *Lifecycle) Stop() error {
    l.state = StateStopped
    return nil
}