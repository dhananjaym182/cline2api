package openaichat

import (
	"testing"

	"cline-go-proxy/internal/protocol"
)

var _ protocol.Protocol = Protocol{}

func TestProtocolCapabilities(t *testing.T) {
	p := Protocol{}
	if p.Name() != protocol.OpenAIChat {
		t.Fatalf("name = %q", p.Name())
	}
	c := p.Capabilities()
	if !c.Chat || !c.Streaming || !c.Tools || !c.Reasoning || !c.Usage {
		t.Fatalf("capabilities = %+v", c)
	}
	if c.ResponsesCompatible || c.AnthropicCompatible {
		t.Fatalf("chat capabilities must not claim other wire shapes: %+v", c)
	}
}
