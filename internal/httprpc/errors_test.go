package httprpc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSentinelError(t *testing.T) {
	t.Run("converts to FuncError via AsType", func(t *testing.T) {
		sentinel := NewSentinelError("test error", "test.Code")
		fe, ok := errors.AsType[*FuncError](sentinel)
		require.True(t, ok, "should convert")
		assert.Equal(t, "test error", fe.ClientMessage)
		assert.Equal(t, "test.Code", fe.Code)
		assert.False(t, fe.ServerFault)
	})

	t.Run("converts to FuncError via AsType on wrapped", func(t *testing.T) {
		sentinel := NewSentinelError("test error", "test.Code")
		wrapped := fmt.Errorf("wrapped: %w", sentinel)
		fe, ok := errors.AsType[*FuncError](wrapped)
		require.True(t, ok, "should convert")
		assert.Equal(t, "test error", fe.ClientMessage)
		assert.Equal(t, "test.Code", fe.Code)
		assert.False(t, fe.ServerFault)
	})
}

func TestFuncErrorMatchesSentinel(t *testing.T) {
	t.Run("Is matches sentinel by code", func(t *testing.T) {
		sentinel := NewSentinelError("test error", "test.Code")
		fe := &FuncError{ClientMessage: "oops", Code: "test.Code"}
		assert.ErrorIs(t, fe, sentinel)
	})

	t.Run("Is matches sentinel by code on wrapped", func(t *testing.T) {
		sentinel := NewSentinelError("test error", "test.Code")
		fe := &FuncError{ClientMessage: "oops", Code: "test.Code"}
		wrapped := fmt.Errorf("wrapped: %w", fe)
		assert.ErrorIs(t, wrapped, sentinel)
	})

	t.Run("Is returns false for mismatched code", func(t *testing.T) {
		sentinel := NewSentinelError("test error", "test.Code")
		fe := &FuncError{ClientMessage: "oops", Code: "other.Code"}
		assert.NotErrorIs(t, fe, sentinel)
	})
}

func TestResponseErrorMatchesSentinel(t *testing.T) {
	t.Run("Is matches sentinel by code", func(t *testing.T) {
		sentinel := NewSentinelError("test error", "test.Code")
		respErr := &ResponseError{Message: "oops", Code: "test.Code"}
		assert.ErrorIs(t, respErr, sentinel)
	})

	t.Run("Is matches sentinel by code on wrapped", func(t *testing.T) {
		sentinel := NewSentinelError("test error", "test.Code")
		respErr := &ResponseError{Message: "oops", Code: "test.Code"}
		wrapped := fmt.Errorf("wrapped: %w", respErr)
		assert.ErrorIs(t, wrapped, sentinel)
	})

	t.Run("Is returns false for empty code", func(t *testing.T) {
		sentinel := NewSentinelError("test error", "test.Code")
		respErr := &ResponseError{Message: "oops", Code: ""}
		assert.NotErrorIs(t, respErr, sentinel)
	})
}
