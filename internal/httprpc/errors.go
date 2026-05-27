package httprpc

import (
	"fmt"
)

// ErrorCodeMatcher is implemented by error targets to match against an RPC error code.
// The interface can be used inside customized Is() on error types ([error.Is]).
type ErrorCodeMatcher interface {
	MatchesErrorCode(string) bool
}

// SentinelError is an RPC-sendable error value identified by a code. It can be matched with
// [errors.Is] on both server ([FuncError]) and client side ([ResponseError]).
type SentinelError struct {
	msg         string
	code        string
	serverFault bool
}

func (s *SentinelError) Error() string {
	return fmt.Sprintf("[%s] %s", s.code, s.msg)
}

// MatchesErrorCode checks if the sentinel matches the passed code.
func (s *SentinelError) MatchesErrorCode(code string) bool {
	return s.code == code
}

// As converts s into a [*FuncError] target.
func (s *SentinelError) As(target any) bool {
	he, ok := target.(**FuncError)
	if !ok {
		return false
	}
	*he = &FuncError{
		ClientMessage: s.msg,
		Code:          s.code,
		ServerFault:   s.serverFault,
	}
	return true
}

func (s *SentinelError) toErrorResponse() ErrorResponse {
	return ErrorResponse{
		Message:     s.msg,
		Code:        s.code,
		ServerFault: s.serverFault,
	}
}

// NewSentinelError returns a client-fault [SentinelError].
func NewSentinelError(msg, code string) *SentinelError {
	return &SentinelError{
		msg:         msg,
		code:        code,
		serverFault: false,
	}
}

// NewSentinelServerError returns a server-fault [SentinelError].
func NewSentinelServerError(msg, code string) *SentinelError {
	return &SentinelError{
		msg:         msg,
		code:        code,
		serverFault: true,
	}
}
