package api

// Logger is the logging capability provided to a mod.
// Arguments follow the structured key/value convention used by slog:
// Logger.Info("loaded", "version", "1.0.0").
type Logger interface {
	Info(message string, args ...any)
	Warn(message string, args ...any)
	Error(message string, args ...any)
}
