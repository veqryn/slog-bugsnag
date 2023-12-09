package slogbugsnag

import (
	"bytes"
	"context"
	"log/slog"
	"time"
)

var defaultTime = time.Date(2023, 9, 29, 13, 0, 59, 0, time.UTC)

type testHandler struct {
	Ctx     context.Context
	Records []slog.Record
	source  bool
}

func (h *testHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *testHandler) Handle(ctx context.Context, r slog.Record) error {
	h.Ctx = ctx
	r.Time = defaultTime
	h.Records = append(h.Records, r)
	return nil
}

func (h *testHandler) WithGroup(string) slog.Handler {
	panic("shouldn't be called")
}

func (h *testHandler) WithAttrs([]slog.Attr) slog.Handler {
	panic("shouldn't be called")
}

// String of latest record
func (h *testHandler) String() string {
	return h.string(len(h.Records) - 1)
}

func (h *testHandler) string(recordIndex int) string {
	if len(h.Records) == 0 {
		return ""
	}
	buf := &bytes.Buffer{}
	err := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: h.source}).Handle(context.Background(), h.Records[recordIndex])
	if err != nil {
		panic(err)
	}
	return buf.String()
}

// MarshalJSON latest record
func (h *testHandler) MarshalJSON() ([]byte, error) {
	return h.marshalJSON(len(h.Records) - 1)
}

func (h *testHandler) marshalJSON(recordIndex int) ([]byte, error) {
	if len(h.Records) == 0 {
		return nil, nil
	}
	buf := &bytes.Buffer{}
	err := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: h.source}).Handle(context.Background(), h.Records[recordIndex])
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
