package slogbugsnag

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bugsnag/bugsnag-go/v2"
)

func init() {
	bugsnag.Configure(bugsnag.Configuration{
		APIKey: "1234567890abcdef1234567890abcdef", // Should be set by env var, 32 characters

		Endpoints: bugsnag.Endpoints{ // Only need to replace in unit tests
			Notify:   "replaceme",
			Sessions: "replaceme",
		},

		ReleaseStage: "test",    // Should be set by env var
		AppType:      "library", // Should be hardcoded in main (cron/service/job/worker-pool/router/etc)
		AppVersion:   "0.0.1",   // version or date + git-commit-sha? Should be set through build tags or env var

		// AutoCaptureSessions: true, // Default
		// Hostname:            "",   // device.GetHostname(), // Default

		NotifyReleaseStages: []string{"production", "test"},           // Which stages to notify in (ie: not development). Can be hardcoded. Default nil.
		ProjectPackages:     []string{"main", "github.com/veqryn/**"}, // Defaults to "main*". Allows SourceRoot trimming and also trims the package prefix as well.
		// SourceRoot:          "/Users/myuser",                          // Defaults to GOPATH or GOROOT. Is trimmed off callstack before project packages are trimmed. Should set with env var: BUGSNAG_SOURCE_ROOT

		ParamsFilters: []string{"password", "secret", "authorization", "cookie", "access_token", "apiKey"}, // Redaction filter for metadata keys

		// PanicHandler: defaultPanicHandler,                           // Default
		// Logger:       log.New(os.Stdout, log.Prefix(), log.Flags()), // Default. Should be set in main or hardcoded to slog
		// Transport:    http.DefaultTransport,                         // Default
		// Synchronous:  false,                                         // Default. Should be set on individual calls to notify.
	})
}

type nested struct {
	Foo string
	Bar struct {
		Baz string
		Qux struct {
			Nuu bool
		}
	}
}

type bugsnagPayload struct {
	// ApiKey string `json:"apiKey"`
	Events []bugsnagEvent `json:"events"`
	// Notifier struct {
	// 	Name    string `json:"name"`
	// 	URL     string `json:"url"`
	// 	Version string `json:"version"`
	// } `json:"notifier"`
}

type bugsnagEvent struct {
	// App struct {
	// 	ReleaseStage string `json:"releaseStage"`
	// 	Type         string `json:"type"`
	// 	Version      string `json:"version"`
	// } `json:"app"`
	Context string `json:"context"`
	// Device  struct {
	// 	Hostname        string `json:"hostname"`
	// 	OsName          string `json:"osName"`
	// 	RuntimeVersions struct {
	// 		Go string `json:"go"`
	// 	} `json:"runtimeVersions"`
	// } `json:"device"`
	Request struct {
		HTTPMethod string `json:"httpMethod"`
		URL        string `json:"url"`
	} `json:"request"`
	Exceptions []struct {
		ErrorClass string `json:"errorClass"`
		Message    string `json:"message"`
		// Stacktrace []struct {
		// 	Method     string `json:"method"`
		// 	File       string `json:"file"`
		// 	LineNumber int    `json:"lineNumber"`
		// 	InProject  bool   `json:"inProject,omitempty"`
		// } `json:"stacktrace"`
	} `json:"exceptions"`
	MetaData map[string]map[string]any `json:"metaData"`
	// PayloadVersion string                    `json:"payloadVersion"`
	// Session        struct {
	// 	StartedAt time.Time `json:"startedAt"`
	// 	ID        string    `json:"id"`
	// 	Events    struct {
	// 		Handled   int `json:"handled"`
	// 		Unhandled int `json:"unhandled"`
	// 	} `json:"events"`
	// } `json:"session"`
	Severity string `json:"severity"`
	// SeverityReason struct {
	// 	Type string `json:"type"`
	// } `json:"severityReason"`
	Unhandled bool `json:"unhandled"`
	User      struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
}

func TestLogToBug(t *testing.T) {
	t.Parallel()

	// Set expectation payload
	expectation := bugsnagPayload{
		Events: []bugsnagEvent{{
			Context: "main message",
			Request: struct {
				HTTPMethod string `json:"httpMethod"`
				URL        string `json:"url"`
			}{
				HTTPMethod: "POST",
				URL:        "http://myserver.com/api/someroute",
			},
			Exceptions: []struct {
				ErrorClass string `json:"errorClass"`
				Message    string `json:"message"`
			}{{
				ErrorClass: "*errors.errorString",
				Message:    "terrible error",
			}},
			MetaData: map[string]map[string]any{
				"log": {
					"time":   "2023-09-29T13:00:59Z",
					"level":  "ERROR",
					"source": "replaceme",
					"msg":    "main message",
					"err":    "terrible error",
					"bool":   true,
					"myid":   "67890",
					"myuser": map[string]any{
						"id": "12345",
					},
				},
				"group1": {
					"float":  123.456,
					"myname": "john",
					"struct": map[string]any{
						"Foo": "foo1",
						"Bar": map[string]any{
							"Baz": "baz1",
							"Qux": map[string]any{
								"Nuu": false,
							},
						},
					},
				},
				"group2": {
					"dur":      "6m0s",
					"time":     "2023-09-29T13:00:59Z",
					"password": "[FILTERED]",
					"myemail":  "j@a.com",
				},
			},
			Severity:  "error",
			Unhandled: true,
			User: struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
			}{
				ID:    "12345",
				Name:  "john",
				Email: "j@a.com",
			},
		}},
	}

	// Create a real but temporary server
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
			expectation.Events[0].MetaData["log"]["source"] = payload.Events[0].MetaData["log"]["source"]

			if !reflect.DeepEqual(payload, expectation) {
				t.Errorf("%#+v\n", payload)
			}
			receivedCall.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer svr.Close()

	// Set the bugsnag config to send all communication to the test server
	notifier := bugsnag.New(bugsnag.Configuration{
		Endpoints: bugsnag.Endpoints{
			Notify:   svr.URL,
			Sessions: svr.URL,
		},
	})

	// Temporary handler
	h := Handler{
		unhandledLevel: slog.LevelError,
		notifier:       notifier,
	}

	// Set up the log contents
	pc, _, _, _ := runtime.Caller(1)

	req, err := http.NewRequest("POST", "http://myserver.com/api/someroute", nil)
	if err != nil {
		t.Fatal("Unable to create fake http request")
	}

	ctx := bugsnag.StartSession(context.Background())
	ctx = bugsnag.AttachRequestData(ctx, req)

	id := ID("67890")
	user := bugsnag.User{Id: "12345"}
	name := Name("john")
	email := Email("j@a.com")

	errForBugsnag := errors.New("terrible error")

	attrs := []slog.Attr{
		slog.Any("err", errForBugsnag),
		slog.Bool("bool", true),
		slog.Any("myid", id),
		slog.Any("myuser", user),
		slog.Group("group1",
			slog.Float64("float", 123.456),
			slog.Any("myname", name),
			slog.Any("struct", nested{
				Foo: "foo1",
				Bar: struct {
					Baz string
					Qux struct{ Nuu bool }
				}{
					Baz: "baz1",
					Qux: struct{ Nuu bool }{
						Nuu: false,
					},
				},
			}),
			slog.Group("group2",
				slog.Time("time", defaultTime),
				slog.Duration("dur", 6*time.Minute),
				slog.String("password", "abc123"),
				slog.Any("myemail", email),
			),
		),
	}

	// Call log to bug
	bug := h.logToBug(ctx, defaultTime, slog.LevelError, "main message", pc, attrs)

	// Send the bug to our fake bugsnag server to verify the content
	err = h.notifier.NotifySync(bug.err, true, bug.rawData...)
	if err != nil {
		t.Error("Unable to notify with bug")
	}

	if !receivedCall.Load() {
		t.Error("Test server did not receive call")
	}
}
