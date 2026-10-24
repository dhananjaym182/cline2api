package responses

import (
	"testing"

	"cline-go-proxy/internal/protocol"
)

var _ protocol.Protocol = Protocol{}

func TestProtocolCapabilities(t *testing.T) {
	p := Protocol{}
	if p.Name() != protocol.OpenAIResponses {
		t.Fatalf("name = %q", p.Name())
	}
	c := p.Capabilities()
	if !c.Chat || !c.Streaming || !c.Tools || !c.Reasoning || !c.Usage || !c.ResponsesCompatible {
		t.Fatalf("capabilities = %+v", c)
	}
	if c.AnthropicCompatible {
		t.Fatalf("responses capabilities must not claim Anthropic: %+v", c)
	}
}
