// Package protocol defines the contract shared by wire-protocol adapters.
//
// Ownership rules:
//   - Converters never write HTTP responses or status codes. They return a
//     *protocol.Error carrying the desired status; dispatch maps it.
//   - Dispatch owns the SSE writer and the retry policy. Once stream bytes have
//     been committed to the client, no cross-provider retry is allowed unless
//     the adapter explicitly declares replay support (Replayable).
//   - Tool calls and reasoning fields must be preserved wherever the target
//     protocol can represent them.
//   - Implementations must honor context cancellation and must not leak
//     goroutines.
package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Name identifies a wire protocol.
type Name string

const (
	OpenAIChat      Name = "openai-chat"
	OpenAIResponses Name = "openai-responses"
	Anthropic       Name = "anthropic"
)

// Capabilities advertises what an adapter can do. Unrelated protocols are not
// forced to implement codecs for features they do not support.
type Capabilities struct {
	Chat                bool
	Streaming           bool
	Tools               bool
	Reasoning           bool
	Usage               bool
	ResponsesCompatible bool
	AnthropicCompatible bool
}

// Protocol is the minimal contract every adapter implements.
type Protocol interface {
	Name() Name
	Capabilities() Capabilities
}

// RequestDecoder parses a client request body into protocol-neutral params.
type RequestDecoder interface {
	DecodeRequest(ctx context.Context, body io.Reader) (map[string]any, error)
}

// RequestEncoder serializes protocol-neutral params to an upstream body.
type RequestEncoder interface {
	EncodeRequest(ctx context.Context, params map[string]any, stream bool) ([]byte, error)
}

// BodyBuilder returns an upstream request object without serializing it.
type BodyBuilder interface {
	BuildBody(ctx context.Context, params map[string]any, stream bool) (map[string]any, error)
}

// StreamTransformer converts an upstream stream into the client's protocol.
// The destination writer is supplied by dispatch; implementations must not
// write HTTP status lines or headers.
type StreamTransformer interface {
	TransformStream(ctx context.Context, src io.Reader, dst io.Writer) error
}

// Replayable is an optional marker for protocols that can safely replay a
// stream from the beginning after bytes were already emitted.
type Replayable interface {
	SupportsStreamReplay() bool
}

// Error is a conversion/protocol failure with an intended HTTP status.
type Error struct {
	Op      string
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Op == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// NewError builds a protocol error.
func NewError(op string, status int, message string) *Error {
	return &Error{Op: op, Status: status, Message: message}
}

// HTTPStatus extracts the intended status from a protocol error, or 0.
func HTTPStatus(err error) int {
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Status
	}
	return 0
}

// GetNested walks a decoded JSON object by string keys and integer indexes.
func GetNested(obj map[string]any, keys ...any) any {
	current := any(obj)
	for _, key := range keys {
		switch k := key.(type) {
		case string:
			m, ok := current.(map[string]any)
			if !ok {
				return nil
			}
			current = m[k]
		case int:
			arr, ok := current.([]any)
			if !ok || k < 0 || k >= len(arr) {
				return nil
			}
			current = arr[k]
		default:
			return nil
		}
	}
	return current
}

// SanitizeContent normalizes assistant text. It is currently a pass-through,
// but adapters call it so future filtering stays in one place.
func SanitizeContent(s string) string { return s }
