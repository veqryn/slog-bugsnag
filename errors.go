package slogbugsnag

import (
	"errors"
	"runtime"

	bserrors "github.com/bugsnag/bugsnag-go/v2/errors"
	perrors "github.com/pkg/errors"
)

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

// newErrorWithStack ensures we have a non-nil error that includes a full stack
// trace, using either the one it came with or generating one from the log line
func newErrorWithStack(errForBugsnag error, msg string, pc uintptr) error {
	// Ensure the error is not nil. Use the log message for the error if not.
	if errForBugsnag == nil {
		errForBugsnag = errors.New(msg)
	}

	// Ensure our error has a caller/stack/frame trace
	switch errForBugsnag.(type) {
	case *bserrors.Error, withCallers, withBSStackFrames, withPStackTrace:
		// Do nothing, these errors already have a full stack
		return errForBugsnag
	}

	// Recreate the callers stack trace, based on the log program counter
	stack := make([]uintptr, bserrors.MaxStackDepth)
	length := runtime.Callers(0, stack[:])
	stack = stack[:length]

	// Iterate until we find our log line program counter, then return a
	// wrapped error with the remaining stack callers
	for idx, ptr := range stack {
		if ptr == pc {
			return errorWithCallers{
				error: errForBugsnag,
				stack: stack[idx:],
			}
		}
	}

	// This can only happen if a handler edited the PC. In that case, let
	// bugsnag create a full stack trace, which will include the log handlers.
	return errForBugsnag
}
