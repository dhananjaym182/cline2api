package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
)

type minimal struct{}

func (minimal) Name() Name                 { return OpenAIChat }
func (minimal) Capabilities() Capabilities { return Capabilities{Chat: true, Streaming: true} }

func TestProtocolContractIsMinimal(t *testing.T) {
	var p Protocol = minimal{}
	if p.Name() != OpenAIChat || !p.Capabilities().Chat {
		t.Fatalf("protocol = %+v", p)
	}
	// Optional codecs must not be required by the base contract.
	var _ = p
	var _ RequestDecoder = (*decoder)(nil)
	var _ RequestEncoder = (*encoder)(nil)
	var _ StreamTransformer = (*streamer)(nil)
	var _ Replayable = (*replay)(nil)
}

type decoder struct{}

func (*decoder) DecodeRequest(ctx context.Context, body io.Reader) (map[string]any, error) {
	return nil, nil
}

type encoder struct{}

func (*encoder) EncodeRequest(ctx context.Context, params map[string]any, stream bool) ([]byte, error) {
	return nil, nil
}

type streamer struct{}

func (*streamer) TransformStream(ctx context.Context, src io.Reader, dst io.Writer) error { return nil }

type replay struct{}

func (*replay) SupportsStreamReplay() bool { return true }

func TestErrorStatus(t *testing.T) {
	err := NewError("convert", 400, "bad request")
	if err.Error() != "convert: bad request" {
		t.Fatalf("error = %q", err.Error())
	}
	if got := HTTPStatus(err); got != 400 {
		t.Fatalf("status = %d", got)
	}
	wrapped := fmt.Errorf("wrap: %w", err)
	if got := HTTPStatus(wrapped); got != 400 {
		t.Fatalf("wrapped status = %d", got)
	}
	plain := errors.New("plain")
	if got := HTTPStatus(plain); got != 0 {
		t.Fatalf("plain status = %d", got)
	}
	if e := (&Error{Message: "x"}); e.Error() != "x" {
		t.Fatalf("bare error = %q", e.Error())
	}
}

func TestGetNested(t *testing.T) {
	obj := map[string]any{
		"choices": []any{
			map[string]any{"message": map[string]any{"content": "hi"}},
		},
	}
	if got := GetNested(obj, "choices", 0, "message", "content"); got != "hi" {
		t.Fatalf("nested = %v", got)
	}
	if got := GetNested(obj, "choices", 9); got != nil {
		t.Fatalf("out of range = %v", got)
	}
	if got := GetNested(obj, "choices", -1); got != nil {
		t.Fatalf("negative = %v", got)
	}
	if got := GetNested(obj, "missing", 0); got != nil {
		t.Fatalf("missing = %v", got)
	}
	if got := GetNested(obj, 3.5); got != nil {
		t.Fatalf("bad key = %v", got)
	}
}

func TestSanitizeContent(t *testing.T) {
	if got := SanitizeContent("hello\x00"); got != "hello\x00" {
		t.Fatalf("content = %q", got)
	}
}
