package slogbugsnag

import (
	"context"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bugsnag/bugsnag-go/v2"
)

// NotifierOptions are options for NotifierWorkers
type NotifierOptions struct {
	// Notifier is the bugsnag notifier that will be used. It should be
	// configured, and may contain custom rawData added to all events.
	// If nil, a default one will be created.
	Notifier *bugsnag.Notifier

	// MaxNotifierConcurrency sets the maximum number of bugs that can be sent
	// to bugsnag in parallel. It defaults to the number of CPU's.
	// Bugs are placed on a buffered channel to be sent to bugsnag, in order
	// to not block or delay the log call from returning. The bugs are then
	// sent to bugsnag synchronously by a number of workers equal to this int.
	MaxNotifierConcurrency int
}

// NotifierWorkers can run a worker pool, where each worker
// synchronously sends bugs to bugsnag. This gives us the ability to flush all
// bugs before terminating an application, by calling [NotifierWorkers.Close]
type NotifierWorkers struct {
	notifier *bugsnag.Notifier
	workerWG sync.WaitGroup
	bugsCh   chan bugRecord
	isClosed atomic.Bool
}

// NewNotifierWorkers creates and starts a worker pool, where each worker
// synchronously sends bugs to bugsnag. This gives us the ability to flush all
// bugs before terminating an application, by calling [NotifierWorkers.Close]
func NewNotifierWorkers(opts *NotifierOptions) *NotifierWorkers {
	if opts == nil {
		opts = &NotifierOptions{}
	}
	if opts.MaxNotifierConcurrency < 1 {
		opts.MaxNotifierConcurrency = runtime.NumCPU()
	}
	if opts.Notifier == nil {
		opts.Notifier = bugsnag.New()
	}

	workers := &NotifierWorkers{
		notifier: opts.Notifier,
		bugsCh:   make(chan bugRecord, 4000),
		workerWG: sync.WaitGroup{},
		isClosed: atomic.Bool{},
	}

	workers.start(opts.MaxNotifierConcurrency)
	return workers
}

// start runs a number of goroutines that consume from the bugsCh
// and notify bugsnag.
func (nw *NotifierWorkers) start(workerCount int) {
	nw.workerWG.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer nw.workerWG.Done()
			for bug := range nw.bugsCh {
				// Notify Bugsnag. Ignore the error because bugsnag has already logged it.
				_ = nw.notifier.NotifySync(bug.err, true, bug.rawData...)
			}
		}()
	}
}

// closed returns if the NotifierWorkers are closed and not accepting new bugs
func (nw *NotifierWorkers) closed() bool {
	return nw.isClosed.Load()
}

// Close stops the NotifierWorkers from accepting any new bugs to its queue.
// This call will block until all bugs currently queued have been sent.
func (nw *NotifierWorkers) Close() {
	nw.isClosed.Store(true)
	close(nw.bugsCh)
	nw.workerWG.Wait()
}

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

	// Notifiers is a worker pool, where each worker synchronously sends
	// bugs to bugsnag. This gives us the ability to flush all bugs before
	// terminating an application, by calling Close on the pool or the handler.
	// If nil, a default notifier worker pool will be started.
	Notifiers *NotifierWorkers
}

// Handler is a slog.Handler middleware that will automatically send log
// lines to Bugsnag (https://www.bugsnag.com/) if they are at least a certain
// level (Error by default).
// The latest error in the log line is sent as the primary error, and all log
// attributes and the context are put into metadata and user tabs and sent with
// the bug.
// It passes the final record and attributes off to the next handler when finished.
// The Bugsnag V2 library should be configured before any logging is done.
//
//	bugsnag.Configure(bugsnag.Configuration{APIKey: ...})
type Handler struct {
	next           slog.Handler
	goa            *groupOrAttrs
	notifyLevel    slog.Leveler
	unhandledLevel slog.Leveler
	notifiers      *NotifierWorkers
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

// NewHandler creates a slog.Handler middleware that will automatically send log
// lines to Bugsnag (https://www.bugsnag.com/) if they are at least a certain
// level (Error by default).
// The latest error in the log line is sent as the primary error, and all log
// attributes and the context are put into metadata and user tabs and sent with
// the bug.
// It passes the final record and attributes off to the next handler when finished.
// The Bugsnag V2 library should be configured before any logging is done.
//
//	bugsnag.Configure(bugsnag.Configuration{APIKey: ...})
//
// If opts is nil, the default options are used.
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
	if opts.Notifiers == nil {
		opts.Notifiers = NewNotifierWorkers(nil)
	}

	return &Handler{
		next:           next,
		notifyLevel:    opts.NotifyLevel,
		unhandledLevel: opts.UnhandledLevel,
		notifiers:      opts.Notifiers,
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

	// Add attributes back in
	newR.AddAttrs(finalAttrs...)

	// Put on the channel to be sent to bugsnag
	if newR.Level >= h.notifyLevel.Level() && !h.notifiers.closed() {
		select {
		case h.notifiers.bugsCh <- h.logToBug(ctx, newR.Time, newR.Level, newR.Message, newR.PC, finalAttrs):
		default:
			// The buffered channel is full, the workers can't keep up,
			h.logBufferFull(ctx, newR.Message, newR.PC)
		}
	}

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

// Close stops the handler from sending any new bugs after this point to bugsnag,
// but it will continue to pass the log records to the next handler.
// This call will block until all bugs currently queued have been sent.
func (h *Handler) Close() {
	h.notifiers.Close()
}

// logBufferFull sends a log message directly to the next handler to record
// that the buffered channel is full and that the workers can't keep up.
func (h *Handler) logBufferFull(ctx context.Context, originalMsg string, pc uintptr) {
	bsR := slog.Record{
		Time:    time.Now(),
		Message: "slog-bugsnag bug buffer full; increase max concurrency or decrease bugs",
		Level:   slog.LevelError,
		PC:      pc,
	}
	bsR.AddAttrs(slog.String("original", originalMsg))
	_ = h.next.Handle(ctx, bsR)
}
