package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCodeOf(t *testing.T) {
	plain := errors.New("boom")
	tests := []struct {
		name   string
		err    error
		parsed bool
		want   int
	}{
		{name: "nil", err: nil, parsed: true, want: ExitOK},
		{name: "typed wrapped twice", err: fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", &ExitError{Code: ExitConflict, Err: plain})), parsed: true, want: ExitConflict},
		{name: "typed before parse", err: &ExitError{Code: ExitPolicy, Err: plain}, parsed: false, want: ExitPolicy},
		{name: "plain before parse is usage", err: plain, parsed: false, want: ExitUsage},
		{name: "plain after parse is unclassified", err: plain, parsed: true, want: exitUnclassified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, codeOf(tt.err, tt.parsed))
		})
	}
}

func TestExitError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	err := fmt.Errorf("ctx: %w", usageErr(inner))
	assert.ErrorIs(t, err, inner)
	assert.Equal(t, "ctx: inner", err.Error())
}
