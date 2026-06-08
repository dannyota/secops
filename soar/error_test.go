package soar

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(&Error{Status: 404}) {
		t.Error("a 404 Error should be NotFound")
	}
	if !IsNotFound(fmt.Errorf("context: %w", &Error{Status: 404})) {
		t.Error("a wrapped 404 should be NotFound")
	}
	if IsNotFound(&Error{Status: 500}) {
		t.Error("a 500 is not NotFound")
	}
	if IsNotFound(errors.New("plain")) {
		t.Error("a plain error is not NotFound")
	}
	if IsNotFound(nil) {
		t.Error("nil is not NotFound")
	}
}
