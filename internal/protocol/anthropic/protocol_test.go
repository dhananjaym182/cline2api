package anthropic

import (
	"testing"

	"cline-go-proxy/internal/protocol"
)

var _ protocol.Protocol = Protocol{}

func TestProtocolCapabilities(t *testing.T) {
	p := Protocol{}
	if p.Name() != protocol.Anthropic {
		t.Fatalf("name = %q", p.Name())
	}
	c := p.Capabilities()
	if !c.Chat || !c.Streaming || !c.Tools || !c.Reasoning || !c.Usage || !c.AnthropicCompatible {
		t.Fatalf("capabilities = %+v", c)
	}
	if c.ResponsesCompatible {
		t.Fatalf("anthropic capabilities must not claim Responses: %+v", c)
	}
}
