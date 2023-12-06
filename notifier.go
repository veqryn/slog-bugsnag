package slogbugsnag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/bugsnag/bugsnag-go/v2"
	bserrors "github.com/bugsnag/bugsnag-go/v2/errors"
	perrors "github.com/pkg/errors"
)

var _ errorWithCallers = errorWithCallers{} // Validate implements interface

// errorWithCallers exists to let us add a caller stack trace onto any error
// that is missing one, starting at the point where the log method is called.
type errorWithCallers struct {
	error
	stack []uintptr
}

// Callers returns the raw stack frames as returned by runtime.Callers()
func (e errorWithCallers) Callers() []uintptr {
	return e.stack[:]
}

// Unwrap provides compatibility for Go 1.13 error chains.
func (e errorWithCallers) Unwrap() error { return e.error }

// withCallers is an error-with-stack-trace trace interface that
// [github.com/bugsnag/bugsnag-go/v2/errors.Error] supports
type withCallers interface {
	Callers() []uintptr
}

// withCallers is an error-with-stack-trace interface that
// [github.com/bugsnag/bugsnag-go/v2/errors.Error] supports
type withBSStackFrames interface {
	StackFrames() []bserrors.StackFrame
}

// withCallers is an error-with-stack-trace interface that
// [github.com/bugsnag/bugsnag-go/v2/errors.Error] supports
type withPStackTrace interface {
	StackTrace() perrors.StackTrace
}

func (h *Handler) notify(ctx context.Context, t time.Time, lvl slog.Level, msg string, pc uintptr, errForBugsnag error, attrs []slog.Attr) {
	// Exit if record level is less than notify level
	if lvl < h.notifyLevel.Level() {
		return
	}

	// Do we report this bugsnag as unhandled or handled?
	var unhandled bool
	if lvl >= h.unhandledLevel.Level() {
		unhandled = true
	}

	// Ensure the error is not nil. Use the log message for the error if not.
	if errForBugsnag == nil {
		errForBugsnag = errors.New(msg)
	}

	// Format the log source line
	frameStack := runtime.CallersFrames([]uintptr{pc})
	frame, _ := frameStack.Next()
	source := fmt.Sprintf("%s:%d", frame.Function, frame.Line)

	// Ensure our error has a caller/stack/frame trace
	switch e := errForBugsnag.(type) {
	case *bserrors.Error, withCallers, withBSStackFrames, withPStackTrace:
		// Do nothing, these errors already have a full stack
	default:
		// Recreate the callers stack trace, based on the log program counter
		stack := make([]uintptr, bserrors.MaxStackDepth)
		length := runtime.Callers(0, stack[:])
		stack = stack[:length]
		// Iterate until we find our log line program counter, then accumulate the stack callers
		for idx, ptr := range stack {
			if ptr == pc {
				errForBugsnag = errorWithCallers{
					error: e,
					stack: stack[idx:],
				}
				break
			}
		}
	}

	// Create MetaData for all the other information in the log
	md := bugsnag.MetaData{}
	h.accumulateMetaData(md, "log", attrs)

	// Add in the log record info
	md.Add("log", "time", t.Format(time.RFC3339Nano))
	md.Add("log", "level", lvl.String())
	md.Add("log", "msg", msg)
	md.Add("log", "source", source)

	// Notify Bugsnag. Ignore the error because bugsnag has already logged it.
	_ = h.notifier.NotifySync(
		errForBugsnag,
		true, // TODO: Buffered Channel + worker pool
		ctx,
		bugsnag.Context{String: msg},
		bugsnag.HandledState{Unhandled: unhandled},
		bsSeverity(lvl),
		md,
	)
}

// accumulateMetaData recursively iterates through all attributes and turns them
// into [bugsnag.MetaData] tabs. The log tab is used for all root-level attributes.
// All attributes in groups get their own tab, named after the group.
// Attribute values are redacted based on the notifier config ParamsFilters.
func (h *Handler) accumulateMetaData(md bugsnag.MetaData, tab string, attrs []slog.Attr) {
	san := sanitizer{Filters: h.notifier.Config.ParamsFilters}

	for _, attr := range attrs {
		if attr.Value.Kind() == slog.KindGroup {
			h.accumulateMetaData(md, attr.Key, attr.Value.Group())

		} else {
			if shouldRedact(attr.Key, h.notifier.Config.ParamsFilters) {
				md.Add(tab, attr.Key, "[FILTERED]")

			} else {
				val := san.Sanitize(attr.Value.Resolve().Any())
				md.Add(tab, attr.Key, val)
			}
		}
	}
}

// bsSeverity converts a [slog.Level] to a [bugsnag.severity]
func bsSeverity(lvl slog.Level) any {
	if lvl < slog.LevelWarn {
		return bugsnag.SeverityInfo
	}
	if lvl < slog.LevelError {
		return bugsnag.SeverityWarning
	}
	return bugsnag.SeverityError
}
