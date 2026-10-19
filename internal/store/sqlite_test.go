package store

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBusy(t *testing.T) {
	assert.False(t, isBusy(errors.New("boom")))
	assert.False(t, isBusy(nil))
}
