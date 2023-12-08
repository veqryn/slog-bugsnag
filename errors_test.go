package slogbugsnag

import (
	"runtime"
	"testing"

	perrors "github.com/pkg/errors"
)

func TestNewErrorWithStackNil(t *testing.T) {
	t.Parallel()

	pc, _, _, _ := runtime.Caller(1)
	err := newErrorWithStack(nil, "oh no", pc+1)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	e, ok := err.(errorWithCallers)
	if !ok {
		t.Fatalf("expected errorWithCallers; Got: %T", err)
	}

	t.Log(e.String())

	if e.Unwrap().Error() != "oh no" || e.Unwrap() != e.error || e.error.Error() != e.Error() {
		t.Error("wrong error:", e.Unwrap().Error())
	}

	if len(e.Callers()) < 2 {
		t.Error("expected larger stack")
	}
}

func TestNewErrorWithStackExisting(t *testing.T) {
	t.Parallel()

	origErr := perrors.New("an error")
	err := newErrorWithStack(origErr, "oh no", 0)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	if err != origErr {
		t.Fatal("expected github.com/pkg/errors error")
	}
}
