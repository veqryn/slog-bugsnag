package slogbugsnag

import (
	"encoding"
	"fmt"
	"reflect"
	"strings"
	"time"
)

/*
This code is copied from github.com/bugsnag/bugsnag-go because it is private and we need it.
It has been modified to support well known types like error, time, and stringers.
*/

// Sanitizer is used to remove filtered params and recursion from meta-data.
type sanitizer struct {
	Filters []string
	Seen    []any
}

// Sanitize resolves any interface into a value that bugsnag can display,
// as well as removing filtered params and recursion from meta-data.
func (s sanitizer) Sanitize(data any) any {
	for _, s := range s.Seen {
		// TODO: we don't need deep equal here, just type-ignoring equality
		if reflect.DeepEqual(data, s) {
			return "[RECURSION]"
		}
	}

	// Sanitizers are passed by value, so we can modify s and it only affects
	// s.Seen for nested calls.
	s.Seen = append(s.Seen, data)

	// Handle certain well known interfaces and types
	switch data := data.(type) {
	case error:
		return data.Error()

	case time.Time:
		return data.Format(time.RFC3339Nano)

	case fmt.Stringer:
		// This also covers time.Duration
		return data.String()

	case encoding.TextUnmarshaler:
		var b []byte
		if err := data.UnmarshalText(b); err == nil {
			return string(b)
		}
	}

	t := reflect.TypeOf(data)
	v := reflect.ValueOf(data)

	if t == nil {
		return "<nil>"
	}

	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return data

	case reflect.String:
		return data

	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			return "<nil>"
		}
		return s.Sanitize(v.Elem().Interface())

	case reflect.Array, reflect.Slice:
		ret := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			ret[i] = s.Sanitize(v.Index(i).Interface())
		}
		return ret

	case reflect.Map:
		return s.sanitizeMap(v)

	case reflect.Struct:
		return s.sanitizeStruct(v, t)

		// Things JSON can't serialize:
		// case t.Chan, t.Func, reflect.Complex64, reflect.Complex128, reflect.UnsafePointer:
	default:
		return "[" + t.String() + "]"
	}
}

func (s sanitizer) sanitizeMap(v reflect.Value) any {
	ret := make(map[string]any)

	for _, key := range v.MapKeys() {
		val := s.Sanitize(v.MapIndex(key).Interface())
		newKey := fmt.Sprintf("%v", key.Interface())

		if s.shouldRedact(newKey) {
			val = "[FILTERED]"
		}

		ret[newKey] = val
	}

	return ret
}

func (s sanitizer) sanitizeStruct(v reflect.Value, t reflect.Type) any {
	ret := make(map[string]any)

	for i := 0; i < v.NumField(); i++ {

		val := v.Field(i)
		// Don't export private fields
		if !val.CanInterface() {
			continue
		}

		name := t.Field(i).Name
		var opts tagOptions

		// Parse JSON tags. Supports name and "omitempty"
		if jsonTag := t.Field(i).Tag.Get("json"); len(jsonTag) != 0 {
			name, opts = parseTag(jsonTag)
		}

		if s.shouldRedact(name) {
			ret[name] = "[FILTERED]"
		} else {
			sanitized := s.Sanitize(val.Interface())
			if str, ok := sanitized.(string); ok {
				if !(opts.Contains("omitempty") && len(str) == 0) {
					ret[name] = str
				}
			} else {
				ret[name] = sanitized
			}
		}
	}

	return ret
}

func (s sanitizer) shouldRedact(key string) bool {
	return shouldRedact(key, s.Filters)
}

func shouldRedact(key string, filters []string) bool {
	for _, filter := range filters {
		if strings.Contains(strings.ToLower(key), strings.ToLower(filter)) {
			return true
		}
	}
	return false
}
