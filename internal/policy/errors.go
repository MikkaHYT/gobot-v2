package policy

import "errors"

var (
	ErrPolicyUnavailable = errors.New("policy storage is unavailable")
	ErrProtectedCommand  = errors.New("cannot disable core command")
	ErrAlreadyRestricted = errors.New("target is already restricted")
	ErrNotRestricted     = errors.New("target is not restricted")
	ErrWordTooLong       = errors.New("word exceeds maximum length of 60 characters")
	ErrRegexTooLong      = errors.New("regex exceeds maximum length of 260 characters")
	ErrRegexInvalid      = errors.New("invalid regex syntax")
	ErrLimitReached      = errors.New("maximum item limit reached")
	ErrTargetRequired    = errors.New("target ID is required")
	ErrGuildRequired     = errors.New("guild ID is required")
	ErrWordRequired      = errors.New("word cannot be empty")
	ErrRegexRequired     = errors.New("regex pattern cannot be empty")
	ErrAlreadyExempt     = errors.New("role is already exempt")
	ErrNotExempt         = errors.New("role is not exempt")
	ErrUnsupportedAction = errors.New("unsupported punishment action")
	ErrInvalidScope      = errors.New("invalid or empty scope type")
)

type RegexError struct {
	err error
}

func (e *RegexError) Error() string {
	if e == nil || e.err == nil {
		return ErrRegexInvalid.Error()
	}
	return e.err.Error()
}

func (e *RegexError) Unwrap() error {
	return ErrRegexInvalid
}
