package slogbugsnag

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bugsnag/bugsnag-go/v2"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	// Set expectation payload
	expectation := bugsnagPayload{
		Events: []bugsnagEvent{{
			Context: "main message",
			Exceptions: []struct {
				ErrorClass string `json:"errorClass"`
				Message    string `json:"message"`
			}{{
				ErrorClass: "slogbugsnag.errorWithCallers",
				Message:    "main message",
			}, {
				ErrorClass: "*errors.errorString",
				Message:    "main message",
			}},
			MetaData: map[string]map[string]any{
				"log": {
					"time":   "2023-09-29T13:00:59Z",
					"level":  "ERROR",
					"source": "github.com/veqryn/slog-bugsnag.TestHandler:97",
					"msg":    "main message",
					"with1":  "arg0",
				},
				"group1": {
					"main1": "arg0",
				},
			},
			Severity:  "error",
			Unhandled: false,
		}},
	}

	receivedCall := atomic.Bool{}
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fmt.Println("IN TEST SERVER")
		if r.Body != nil {
			defer r.Body.Close()
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error("Unable to read body:", err)
			}
			// t.Log("RECEIVED:", string(b))

			var payload bugsnagPayload
			err = json.Unmarshal(b, &payload)
			if err != nil {
				t.Error("Unable to unmarshal json to bugsnag payload")
			}

			// Replace source field since it changes
			t.Log(payload.Events[0].MetaData["log"]["source"])
			expectation.Events[0].MetaData["log"]["time"] = payload.Events[0].MetaData["log"]["time"]

			if !reflect.DeepEqual(payload, expectation) {
				t.Errorf("%#+v\n", payload)
			}
			receivedCall.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer svr.Close()

	opts := &HandlerOptions{
		Notifier: bugsnag.New(bugsnag.Configuration{
			Endpoints: bugsnag.Endpoints{
				Notify:   svr.URL,
				Sessions: svr.URL,
			},
		}),
	}

	tester := &testHandler{}
	h := NewMiddleware(opts)(tester)

	log := slog.New(h)
	log = log.With().WithGroup("")
	log = log.With("with1", "arg0")
	log = log.WithGroup("group1")
	log.Error("main message", "main1", "arg0")

	expectedLog := `time=2023-09-29T13:00:59.000Z level=ERROR msg="main message" with1=arg0 group1.main1=arg0`
	if strings.TrimSpace(tester.String()) != expectedLog {
		t.Error("Received:", tester.String())
	}

	// Flush the channel and workers
	h.(*Handler).Close()

	if !receivedCall.Load() {
		t.Error("Test server did not receive call")
	}
}
