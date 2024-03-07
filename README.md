# slog-bugsnag
[![tag](https://img.shields.io/github/tag/veqryn/slog-bugsnag.svg)](https://github.com/veqryn/slog-bugsnag/releases)
![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.21-%23007d9c)
[![GoDoc](https://godoc.org/github.com/veqryn/slog-bugsnag?status.svg)](https://pkg.go.dev/github.com/veqryn/slog-bugsnag)
![Build Status](https://github.com/veqryn/slog-bugsnag/actions/workflows/build_and_test.yml/badge.svg)
[![Go report](https://goreportcard.com/badge/github.com/veqryn/slog-bugsnag)](https://goreportcard.com/report/github.com/veqryn/slog-bugsnag)
[![Coverage](https://img.shields.io/codecov/c/github/veqryn/slog-bugsnag)](https://codecov.io/gh/veqryn/slog-bugsnag)
[![Contributors](https://img.shields.io/github/contributors/veqryn/slog-bugsnag)](https://github.com/veqryn/slog-bugsnag/graphs/contributors)
[![License](https://img.shields.io/github/license/veqryn/slog-bugsnag)](./LICENSE)

Golang structured logging (slog) handler middleware for Bugsnag.
Automatically send all Error level logs to Bugsnag, along with all attributes and context.
Never forget to snag another bug again.

### Other Great SLOG Utilities
- [slogctx](https://github.com/veqryn/slog-context): Add attributes to context and have them automatically added to all log lines. Work with a logger stored in context.
- [slogotel](https://github.com/veqryn/slog-context/tree/main/otel): Automatically extract and add [OpenTelemetry](https://opentelemetry.io/) TraceID's to all log lines.
- [slogdedup](https://github.com/veqryn/slog-dedup): Middleware that deduplicates and sorts attributes. Particularly useful for JSON logging.
- [slogbugsnag](https://github.com/veqryn/slog-bugsnag): Middleware that pipes Errors to [Bugsnag](https://www.bugsnag.com/).

## Install
`go get github.com/veqryn/slog-bugsnag`

```go
import (
	slogbugsnag "github.com/veqryn/slog-bugsnag"
)
```

## Usage
```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/bugsnag/bugsnag-go/v2"
	"github.com/pkg/errors"
	slogbugsnag "github.com/veqryn/slog-bugsnag"
)

func main() {
	// Configure bugsnag
	bugsnag.Configure(bugsnag.Configuration{
		APIKey:     os.Getenv("BUGSNAG_API_KEY"),
		AppVersion: "0.1.0",
	})

	// Setup slog handlers
	h := slogbugsnag.NewHandler(slog.NewJSONHandler(os.Stdout, nil), nil)
	slog.SetDefault(slog.New(h))

	// Optional: closing the handler will flush all bugs in the queue to bugsnag
	defer h.Close()

	slog.Info("starting up...") // Not sent to bugsnag

	err := errors.New("my horrible error")
	slog.Error("oh no...", "err", err) // Sent to bugsnag, with original err's stack trace

	// Can be used with context too
	ctx := bugsnag.StartSession(context.Background())
	defer bugsnag.AutoNotify()

	// Bugsnag can capture basic http.Request data, if added to the ctx
	req, _ := http.NewRequest("GET", "https://www.github.com/veqryn/slog-bugsnag", nil)
	ctx = bugsnag.AttachRequestData(ctx, req)

	// User information can be sent as a bugsnag.User
	user := bugsnag.User{Id: "1234", Name: "joe", Email: "none"}

	// Or using these string types, added to the log as attribute values (any key is fine)
	id := slogbugsnag.ID("1234")
	name := slogbugsnag.Name("joe")
	email := slogbugsnag.Email("none")

	// Tabs will be created in bugsnag for each group,
	// with the root level attributes going into a "log" tab.
	log := slog.With(slog.Any("id", id), slog.Any("name", name), slog.Any("email", email))
	log = log.WithGroup("MyTab")

	// If no "error" type is found among log attribute values,
	// then an error will be created using the log message, with a stack trace at the log call.
	log.ErrorContext(ctx, "more bad things", slog.Any("user", user), slog.String("foo", "bar"))

	// All of the above will log out 3 lines and send 2 bugs reports to bugsnag.
	// The second bug will have tabs for Stacktrace, App, Device, Request, User, Log, and MyTab,
	// containing all the data in the log record.
}
```

### slog-multi Middleware
This library has a convenience method that allow it to interoperate with [github.com/samber/slog-multi](https://github.com/samber/slog-multi),
in order to easily setup slog workflows such as pipelines, fanout, routing, failover, etc.
```go
notifiers := slogbugsnag.NewNotifierWorkers(&slogbugsnag.NotifierOptions{})
defer notifiers.Close()

slog.SetDefault(slog.New(slogmulti.
	Pipe(slogcontext.NewMiddleware(&slogcontext.HandlerOptions{})).
	Pipe(slogdedup.NewOverwriteMiddleware(&slogdedup.OverwriteHandlerOptions{})).
	Pipe(slogbugsnag.NewMiddleware(&slogbugsnag.HandlerOptions{Notifiers: notifiers})).
	Handler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})),
))
```
