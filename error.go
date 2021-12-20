package arpc

import "fmt"

const (
	CodeOK              uint32 = 0
	CodeUnknown                = 1
	CodeServiceNotFound        = 2
	CodeMethodNotFound         = 3
	CodeInvalidRequest         = 4
	CodeConnClosed             = 5
)

type Error struct {
	Code    uint32
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func Errorf(code uint32, format string, args ...interface{}) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

func AsError(err error) *Error {
	if e, ok := err.(*Error); ok {
		return e
	} else {
		return &Error{
			Code:    CodeUnknown,
			Message: err.Error(),
		}
	}
}
