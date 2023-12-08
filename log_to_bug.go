package slogbugsnag

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/bugsnag/bugsnag-go/v2"
)

// bugsnagUserID is a sentinel interface that gives you another option to
// customize how bugsnag fills the ID in the "User" tab
type bugsnagUserID interface {
	BugsnagUserID() string
}

// bugsnagUserName is a sentinel interface that gives you another option to
// customize how bugsnag fills the Name in the "User" tab
type bugsnagUserName interface {
	BugsnagUserName() string
}

// bugsnagUserEmail is a sentinel interface that gives you another option to
// customize how bugsnag fills the Email in the "User" tab
type bugsnagUserEmail interface {
	BugsnagUserEmail() string
}

var _ bugsnagUserID = ID("") // Validate implements interface

// ID is a string that, if used as a log attribute value, will be filled in
// on the [bugsnag.User] struct. The ID is also used to determine the number
// of users affected by the bug, in bugsnag's system.
type ID string

// BugsnagUserID returns the bugsnag user id
func (id ID) BugsnagUserID() string {
	return string(id)
}

var _ bugsnagUserName = Name("") // Validate implements interface

// Name is a string that, if used as a log attribute value, will be filled in
// on the [bugsnag.User] struct.
type Name string

// BugsnagUserName returns the bugsnag user name
func (name Name) BugsnagUserName() string {
	return string(name)
}

var _ bugsnagUserEmail = Email("") // Validate implements interface

// Email is a string that, if used as a log attribute value, will be filled in
// on the [bugsnag.User] struct.
type Email string

// BugsnagUserEmail returns the bugsnag user email
func (email Email) BugsnagUserEmail() string {
	return string(email)
}

// bug type contains everything needed to be sent off to bugsnag, preformatted
type bugRecord struct {
	err     error
	rawData []any
}

// logToBug creates and formats a bug, from a log record and attributes.
// The level of the error should be checked if sufficient or not before calling.
func (h *Handler) logToBug(ctx context.Context, t time.Time, lvl slog.Level, msg string, pc uintptr, attrs []slog.Attr) bugRecord {
	// Do we report this bugsnag as unhandled or handled?
	var unhandled bool
	if lvl >= h.unhandledLevel.Level() {
		unhandled = true
	}

	// Format the log source line
	frameStack := runtime.CallersFrames([]uintptr{pc})
	frame, _ := frameStack.Next()
	source := fmt.Sprintf("%s:%d", frame.Function, frame.Line)

	// Find the errors and bugsnag.User's in the log attributes.
	// Create MetaData for all the other information in the log.
	var errForBugsnag error
	user := bugsnag.User{}
	md := bugsnag.MetaData{}
	h.accumulateRawData(&errForBugsnag, &user, md, "log", attrs)

	// Add in the log record info
	md.Add("log", "time", t.Format(time.RFC3339Nano))
	md.Add("log", "level", lvl.String())
	md.Add("log", "msg", msg)
	md.Add("log", "source", source)

	// Ensure the error is not nil and has a stack trace
	errForBugsnag = newErrorWithStack(errForBugsnag, msg, pc)

	// The order matters
	rawData := []any{
		ctx,
		bugsnag.Context{String: msg},
		bugsnag.HandledState{Unhandled: unhandled},
		bsSeverity(lvl), // Must come after HandledState
		md,
	}
	if user.Id != "" || user.Name != "" || user.Email != "" {
		rawData = append(rawData, user)
	}

	return bugRecord{err: errForBugsnag, rawData: rawData}
}

// accumulateRawData recursively iterates through all attributes and turns them
// into [bugsnag.MetaData] tabs. The log tab is used for all root-level attributes.
// All attributes in groups get their own tab, named after the group.
// Attribute values are redacted based on the notifier config ParamsFilters.
// accumulateRawData also finds the latest [error] and [bugsnag.User].
func (h *Handler) accumulateRawData(errForBugsnag *error, user *bugsnag.User, md bugsnag.MetaData, tab string, attrs []slog.Attr) {
	san := sanitizer{Filters: h.notifier.Config.ParamsFilters}

	for _, attr := range attrs {
		if attr.Value.Kind() == slog.KindGroup {
			h.accumulateRawData(errForBugsnag, user, md, attr.Key, attr.Value.Group())
			continue
		}

		// Because the attributes slice we are iterating through is ordered from
		// oldest to newest, we should overwrite the error/user to get the latest one.
		// Because there could be multiple, we still add these to the MetaData map.
		switch t := attr.Value.Any().(type) {
		case error:
			if t != nil {
				*errForBugsnag = t
			}

		case bugsnag.User:
			*user = t

		case bugsnagUserID:
			user.Id = t.BugsnagUserID()

		case bugsnagUserName:
			user.Name = t.BugsnagUserName()

		case bugsnagUserEmail:
			user.Email = t.BugsnagUserEmail()
		}

		// Replace with filtered if the key matches
		if shouldRedact(attr.Key, h.notifier.Config.ParamsFilters) {
			md.Add(tab, attr.Key, "[FILTERED]")
			continue
		}

		// Always resolve log attribute values
		attr.Value = attr.Value.Resolve()
		val := san.Sanitize(attr.Value.Any())
		md.Add(tab, attr.Key, val)
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
