package domain

import "errors"

var (
	ErrUnsupportedSignal = errors.New("unsupported signal")
	ErrMalformedPayload  = errors.New("malformed OTLP payload")
	ErrEmptyPayload      = errors.New("empty payload")
	ErrPayloadTooLarge   = errors.New("payload too large")
)
