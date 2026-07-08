package apperr

import (
	"errors"
	"fmt"
)

type Kind string

const (
	KindUnknown  Kind = "unknown"
	KindConfig   Kind = "config"
	KindAuth     Kind = "auth"
	KindNetwork  Kind = "network"
	KindIO       Kind = "io"
	KindCancel   Kind = "cancel"
	KindInternal Kind = "internal"
)

type Error struct {
	Op   string
	Kind Kind
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	if e.Op != "" && e.Kind != "" && e.Err != nil {
		return fmt.Sprintf("%s (%s): %v", e.Op, e.Kind, e.Err)
	}

	if e.Op != "" && e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	}

	if e.Kind != "" && e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Kind, e.Err)
	}

	if e.Err != nil {
		return e.Err.Error()
	}

	if e.Op != "" {
		return e.Op
	}

	if e.Kind != "" {
		return string(e.Kind)
	}

	return "error"
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

func New(op string, kind Kind, err error) error {
	if err == nil {
		return nil
	}

	return &Error{
		Op:   op,
		Kind: kind,
		Err:  err,
	}
}

func Wrap(op string, err error) error {
	if err == nil {
		return nil
	}

	var e *Error
	if errors.As(err, &e) {
		return &Error{
			Op:   op,
			Kind: e.Kind,
			Err:  err,
		}
	}

	return &Error{
		Op:   op,
		Kind: KindUnknown,
		Err:  err,
	}
}

func IsKind(err error, kind Kind) bool {
	if err == nil {
		return false
	}

	var e *Error
	if !errors.As(err, &e) {
		return false
	}

	return e.Kind == kind
}
