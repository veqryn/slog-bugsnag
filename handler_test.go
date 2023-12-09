package slogbugsnag

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
					"source": "github.com/veqryn/slog-bugsnag.TestHandler:102",
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

	// Set the bugsnag config to send all communication to the test server
	notifiers := NewNotifierWorkers(&NotifierOptions{
		Notifier: bugsnag.New(bugsnag.Configuration{
			Endpoints: bugsnag.Endpoints{
				Notify:   svr.URL,
				Sessions: svr.URL,
			},
		}),
	})

	opts := &HandlerOptions{Notifiers: notifiers}

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

func TestHandlerOverloaded(t *testing.T) {
	t.Parallel()

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer svr.Close()

	// Set the bugsnag config to send all communication to the test server
	notifiers := &NotifierWorkers{
		notifier: bugsnag.New(bugsnag.Configuration{
			Endpoints: bugsnag.Endpoints{
				Notify:   svr.URL,
				Sessions: svr.URL,
			},
		}),
		bugsCh:   make(chan bugRecord, 1),
		workerWG: sync.WaitGroup{},
		isClosed: atomic.Bool{},
	}
	notifiers.start(1)

	tester := &testHandler{}
	h := NewHandler(tester, &HandlerOptions{Notifiers: notifiers})
	log := slog.New(h)

	log.Error("this will be sent to fake bugsnag")

	// Depending on race conditions and available threads, we may need to log twice to trigger it, as one may sit in the buffer
	log.Error("this could trigger an extra log line about handler/workers overloaded")
	log.Error("this definitely should trigger an extra log line about handler/workers overloaded")

	// Flush the channel and workers
	h.Close()

	// level=ERROR msg="slog-bugsnag bug buffer full; increase max concurrency or decrease bugs" original="this should trigger an extra log line about handler/workers overloaded"
	if len(tester.Records) < 4 {
		t.Fatal("Expected at least 4 log records; Got:", tester.Records)
	}

	var found bool
	for idx := range tester.Records {
		if strings.Contains(tester.string(idx), "slog-bugsnag bug buffer full; increase max concurrency or decrease bugs") {
			found = true
		}
	}
	if !found {
		t.Error("Expected a log line about bug buffer full; Got:", tester.Records)
	}
}
