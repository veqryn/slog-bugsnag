package slogbugsnag

import (
	"context"
	"log/slog"
	"slices"

	"github.com/bugsnag/bugsnag-go/v2"
)

// HandlerOptions are options for a Handler
type HandlerOptions struct {

	// Level reports the minimum record level that will be sent to bugsnag.
	// The handler ignores but still passes along records with lower levels
	// to the next handler.
	// If NotifyLevel is nil, the handler assumes LevelError.
	// The handler calls NotifyLevel.Level() for each record processed;
	// to adjust the minimum level dynamically, use a LevelVar.
	NotifyLevel slog.Leveler

	// UnhandledLevel reports the minimum record level that will be sent to
	// bugsnag as an unhandled error.
	// If UnhandledLevel is nil, the handler assumes slog.LevelError + 4.
	UnhandledLevel slog.Leveler

	// Notifier is the bugsnag notifier that will be used. It should be
	// configured, and may contain custom rawData added to all events.
	Notifier *bugsnag.Notifier
}

// Handler is a slog.Handler middleware that will ...
type Handler struct {
	next           slog.Handler
	goa            *groupOrAttrs
	notifyLevel    slog.Leveler
	unhandledLevel slog.Leveler
	notifier       *bugsnag.Notifier
}

var _ slog.Handler = &Handler{} // Assert conformance with interface

// NewMiddleware creates a slogbugsnag.Handler slog.Handler middleware
// that conforms to [github.com/samber/slog-multi.Middleware] interface.
// It can be used with slogmulti methods such as Pipe to easily setup a pipeline of slog handlers:
//
//	slog.SetDefault(slog.New(slogmulti.
//		Pipe(slogcontext.NewMiddleware(&slogcontext.HandlerOptions{})).
//		Pipe(slogdedup.NewOverwriteMiddleware(&slogdedup.OverwriteHandlerOptions{})).
//		Pipe(slogbugsnag.NewMiddleware(&slogbugsnag.HandlerOptions{})).
//		Handler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})),
//	))
func NewMiddleware(options *HandlerOptions) func(slog.Handler) slog.Handler {
	return func(next slog.Handler) slog.Handler {
		return NewHandler(
			next,
			options,
		)
	}
}

// NewHandler creates a Handler slog.Handler middleware that will ...
// If opts is nil, the default options are used.
// Bugsnag should be configurated before any logging is done.
//
//	bugsnag.Configure(bugsnag.Configuration{APIKey: ...})
func NewHandler(next slog.Handler, opts *HandlerOptions) *Handler {
	if opts == nil {
		opts = &HandlerOptions{}
	}
	if opts.NotifyLevel == nil {
		opts.NotifyLevel = slog.LevelError
	}
	if opts.UnhandledLevel == nil {
		opts.UnhandledLevel = slog.LevelError + 4
	}
	if opts.Notifier == nil {
		opts.Notifier = bugsnag.New()
	}

	return &Handler{
		next:           next,
		notifyLevel:    opts.NotifyLevel,
		unhandledLevel: opts.UnhandledLevel,
		notifier:       opts.Notifier,
	}
}

// Enabled reports whether the next handler handles records at the given level.
// The handler ignores records whose level is lower.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle collects all attributes and groups, then passes the record and its attributes to the next handler.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	// Collect all attributes from the record (which is the most recent attribute set).
	// These attributes are ordered from oldest to newest, and our collection will be too.
	finalAttrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		finalAttrs = append(finalAttrs, a)
		return true
	})

	// Iterate through the goa (group Or Attributes) linked list, which is ordered from newest to oldest
	for g := h.goa; g != nil; g = g.next {
		if g.group != "" {
			// If a group, put all the previous attributes (the newest ones) in it
			finalAttrs = []slog.Attr{{
				Key:   g.group,
				Value: slog.GroupValue(finalAttrs...),
			}}
		} else {
			// Prepend to the front of finalAttrs, because finalAttrs is ordered from oldest to newest
			finalAttrs = append(slices.Clip(g.attrs), finalAttrs...)
		}
	}

	// Add all attributes to new record (because old record has all the old attributes as private members)
	newR := &slog.Record{
		Time:    r.Time,
		Level:   r.Level,
		Message: r.Message,
		PC:      r.PC,
	}

	// Add deduplicated attributes back in
	newR.AddAttrs(finalAttrs...)

	// Notify bugsnag
	h.notify(ctx, newR.Time, newR.Level, newR.Message, newR.PC, finalAttrs)

	// Pass off to the next handler
	return h.next.Handle(ctx, *newR)
}

// WithGroup returns a new AppendHandler that still has h's attributes,
// but any future attributes added will be namespaced.
func (h *Handler) WithGroup(name string) slog.Handler {
	h2 := *h
	h2.goa = h2.goa.WithGroup(name)
	return &h2
}

// WithAttrs returns a new AppendHandler whose attributes consists of h's attributes followed by attrs.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.goa = h2.goa.WithAttrs(attrs)
	return &h2
}
