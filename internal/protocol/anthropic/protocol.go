package anthropic

import "cline-go-proxy/internal/protocol"

// Protocol describes the Anthropic Messages wire shape.
type Protocol struct{}

// Name returns the protocol identifier.
func (Protocol) Name() protocol.Name { return protocol.Anthropic }

// Capabilities reports what the Messages adapter can carry.
func (Protocol) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		Chat:                true,
		Streaming:           true,
		Tools:               true,
		Reasoning:           true,
		Usage:               true,
		AnthropicCompatible: true,
	}
}
