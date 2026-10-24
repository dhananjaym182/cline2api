package responses

import "cline-go-proxy/internal/protocol"

// Protocol describes the OpenAI Responses wire shape.
type Protocol struct{}

// Name returns the protocol identifier.
func (Protocol) Name() protocol.Name { return protocol.OpenAIResponses }

// Capabilities reports what the Responses adapter can carry.
func (Protocol) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		Chat:                true,
		Streaming:           true,
		Tools:               true,
		Reasoning:           true,
		Usage:               true,
		ResponsesCompatible: true,
	}
}
