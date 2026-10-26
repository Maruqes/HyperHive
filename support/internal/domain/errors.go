package domain

type ErrorKind uint8

const (
	InvalidInput ErrorKind = iota
	ConflictState
	MissingResource
	AuthenticationRequired
	ServiceUnavailable
	PayloadTooLarge
)

type Error struct {
	Kind    ErrorKind
	Message string
}

func (err *Error) Error() string        { return err.Message }
func Invalid(message string) error      { return &Error{InvalidInput, message} }
func Conflict(message string) error     { return &Error{ConflictState, message} }
func NotFound() error                   { return &Error{MissingResource, "not found"} }
func Unauthorized(message string) error { return &Error{AuthenticationRequired, message} }
func Unavailable(message string) error  { return &Error{ServiceUnavailable, message} }
func TooLarge(message string) error     { return &Error{PayloadTooLarge, message} }
