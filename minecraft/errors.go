package minecraft

import "errors"

var (
	ErrInvalidState   = errors.New("invalid instance state")
	ErrInvalidCommand = errors.New("invalid command")
)
