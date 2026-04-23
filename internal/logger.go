package internal

import (
	"io"
)

type Logger interface {
	SetOutput(w io.Writer)
	Printf(format string, v ...any)
	Writer() io.Writer
}
