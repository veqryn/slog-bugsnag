package slogbugsnag

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

type _account struct {
	ID   string
	Name string
	Plan struct {
		Premium bool
	}
	Password      string
	secret        string
	Email         string `json:"email"`
	EmptyEmail    string `json:"emptyemail,omitempty"`
	NotEmptyEmail string `json:"not_empty_email,omitempty"`
}

type _broken struct {
	Me   *_broken
	Data string
}

type _textMarshaller struct{}

func (_textMarshaller) MarshalText() ([]byte, error) {
	return []byte("marshalled text"), nil
}

func TestSanitize(t *testing.T) {
	var broken = _broken{}
	broken.Me = &broken
	broken.Data = "ohai"

	account := _account{}
	account.Name = "test"
	account.ID = "test"
	account.secret = "hush"
	account.Email = "example@example.com"
	account.EmptyEmail = ""
	account.NotEmptyEmail = "not_empty_email@example.com"

	data := map[string]map[string]any{
		"one": {
			"bool":     true,
			"int":      7,
			"float":    7.1,
			"complex":  complex(1, 1),
			"func":     func() {},
			"string":   "string",
			"password": "secret",
			"error":    errors.New("some error"),
			"time":     time.Date(2023, 12, 5, 23, 59, 59, 123456789, time.UTC),
			"duration": 105567462 * time.Millisecond,
			"text":     _textMarshaller{},
			"array": []map[string]any{{
				"creditcard": "1234567812345678",
				"broken":     broken,
			}},
			"broken":  broken,
			"account": account,
		},
	}

	san := sanitizer{Filters: []string{"password", "creditcard"}}
	actual := san.Sanitize(data)

	if !reflect.DeepEqual(actual, map[string]any{
		"one": map[string]any{
			"bool":     true,
			"int":      7,
			"float":    7.1,
			"complex":  "[complex128]",
			"string":   "string",
			"func":     "[func()]",
			"password": "[FILTERED]",
			"error":    "some error",
			"time":     "2023-12-05T23:59:59.123456789Z",
			"duration": "29h19m27.462s",
			"text":     "marshalled text",
			"array": []any{map[string]any{
				"creditcard": "[FILTERED]",
				"broken": map[string]any{
					"Me":   "[RECURSION]",
					"Data": "ohai",
				},
			}},
			"broken": map[string]any{
				"Me":   "[RECURSION]",
				"Data": "ohai",
			},
			"account": map[string]any{
				"ID":   "test",
				"Name": "test",
				"Plan": map[string]any{
					"Premium": false,
				},
				"Password":        "[FILTERED]",
				"email":           "example@example.com",
				"not_empty_email": "not_empty_email@example.com",
			},
		},
	}) {
		t.Errorf("metadata.Sanitize didn't work: %#v", actual)
	}
}

func TestSanitizerSanitize(t *testing.T) {
	var (
		nilPointer   *int
		nilInterface = any(nil)
	)

	for n, tc := range []struct {
		input any
		want  any
	}{
		{nilPointer, "<nil>"},
		{nilInterface, "<nil>"},
	} {
		s := &sanitizer{}
		gotValue := s.Sanitize(tc.input)

		if got, want := gotValue, tc.want; got != want {
			t.Errorf("[%d] got %v, want %v", n, got, want)
		}
	}
}
