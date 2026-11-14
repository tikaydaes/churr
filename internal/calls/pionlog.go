package calls

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/pion/logging"
)

// pionLogs gives Pion a logger that writes to the server's own log at the
// debug level and nowhere else. What Pion has to say is mostly about one
// peer's packets and can hold a participant's network address, which the
// server does not log at its default level (docs/CALLS.md section 17).
// Failures that matter to an operator are logged by this package itself.
type pionLogs struct{ log *slog.Logger }

func (p pionLogs) NewLogger(scope string) logging.LeveledLogger {
	return pionLogger{log: p.log, scope: scope}
}

type pionLogger struct {
	log   *slog.Logger
	scope string
}

func (l pionLogger) say(msg string) {
	if l.log.Enabled(context.Background(), slog.LevelDebug) {
		l.log.Debug("pion: "+msg, "scope", l.scope)
	}
}

func (l pionLogger) sayf(format string, args ...any) {
	if l.log.Enabled(context.Background(), slog.LevelDebug) {
		l.log.Debug("pion: "+fmt.Sprintf(format, args...), "scope", l.scope)
	}
}

// Trace, debug and info are dropped: they are a line per packet or per
// connection step.
func (l pionLogger) Trace(string)          {}
func (l pionLogger) Tracef(string, ...any) {}
func (l pionLogger) Debug(string)          {}
func (l pionLogger) Debugf(string, ...any) {}
func (l pionLogger) Info(string)           {}
func (l pionLogger) Infof(string, ...any)  {}

func (l pionLogger) Warn(msg string)                   { l.say(msg) }
func (l pionLogger) Warnf(format string, args ...any)  { l.sayf(format, args...) }
func (l pionLogger) Error(msg string)                  { l.say(msg) }
func (l pionLogger) Errorf(format string, args ...any) { l.sayf(format, args...) }
