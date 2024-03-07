package slogbugsnag_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/slogtest"

	"github.com/bugsnag/bugsnag-go/v2"
	slogbugsnag "github.com/veqryn/slog-bugsnag"
)

func TestSlogtest(t *testing.T) {

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer svr.Close()

	// Set the bugsnag config to send all communication to the test server
	notifiers := slogbugsnag.NewNotifierWorkers(&slogbugsnag.NotifierOptions{
		Notifier: bugsnag.New(bugsnag.Configuration{
			Endpoints: bugsnag.Endpoints{
				Notify:   svr.URL,
				Sessions: svr.URL,
			},
		}),
	})

	opts := &slogbugsnag.HandlerOptions{Notifiers: notifiers}

	var buf bytes.Buffer
	h := slogbugsnag.NewHandler(slog.NewJSONHandler(&buf, nil), opts)

	results := func() []map[string]any {
		ms, err := parseLines(buf.Bytes(), parseJSON)
		if err != nil {
			t.Fatal(err)
		}
		return ms
	}
	if err := slogtest.TestHandler(h, results); err != nil {
		t.Fatal(err)
	}
}

func parseLines(src []byte, parse func([]byte) (map[string]any, error)) ([]map[string]any, error) {
	fmt.Println(string(src))
	var records []map[string]any
	for _, line := range bytes.Split(src, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		m, err := parse(line)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", string(line), err)
		}
		records = append(records, m)
	}
	return records, nil
}

func parseJSON(bs []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(bs, &m); err != nil {
		return nil, err
	}
	return m, nil
}
