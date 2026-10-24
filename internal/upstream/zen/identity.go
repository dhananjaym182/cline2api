package zen

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ClientVersion tracks the latest opencode release
// (packages/opencode/package.json).
const ClientVersion = "1.19.0"

// ProviderUtilsVersions maps each protocol to the ai-sdk provider-utils version
// the released binary uses. The values differ per protocol.
var ProviderUtilsVersions = map[string]string{
	"chat":      "4.0.23",
	"responses": "4.0.40",
	"anthropic": "4.0.46",
}

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Identifier generates opencode-shaped identifiers:
// 12 hex of (Date.now()*0x1000 + per-millisecond counter) (inverted for
// descending order) + 14 random base62 characters. The free tier validates the
// counter nibble (hex [9:12] inverted), so a hash-shaped fake only passes with
// roughly 16/4096 probability.
type Identifier struct {
	mu        sync.Mutex
	lastStamp int64
	counter   uint64
}

// NewIdentifier returns an independent identifier generator.
func NewIdentifier() *Identifier { return &Identifier{} }

// New produces an identifier with the given prefix.
func (g *Identifier) New(prefix string, descending bool) string {
	g.mu.Lock()
	now := time.Now().UnixMilli()
	if now != g.lastStamp {
		g.lastStamp = now
		g.counter = 0
	}
	g.counter++
	g.mu.Unlock()
	current := uint64(now)*0x1000 + g.counter
	value := current & 0xffffffffffff
	if descending {
		value = ^current & 0xffffffffffff
	}
	random := make([]byte, 14)
	rand.Read(random)
	for i, b := range random {
		random[i] = base62Alphabet[int(b)%len(base62Alphabet)]
	}
	return fmt.Sprintf("%s_%012x%s", prefix, value, string(random))
}

// RandHex returns n random bytes hex-encoded.
func RandHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// RandIntn returns a pseudo-random non-negative int below n.
func RandIntn(n int) int {
	if n <= 0 {
		return 0
	}
	b := make([]byte, 4)
	rand.Read(b)
	v := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if v < 0 {
		v = -v
	}
	return v % n
}

// WithRetryJitter adds 0-25% jitter to a backoff delay to spread concurrent
// retries.
func WithRetryJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	return delay + time.Duration(float64(delay)*float64(RandIntn(26))/100)
}

// ConversationSeed extracts a stable conversation seed: the client session
// signal when present, otherwise the first user message content.
func ConversationSeed(params map[string]any) string {
	if meta, ok := params["metadata"].(map[string]any); ok {
		if sid, _ := meta["session_id"].(string); sid != "" {
			return sid
		}
	}
	msgs, ok := params["messages"].([]any)
	if !ok {
		return ""
	}
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok || mm["role"] != "user" {
			continue
		}
		encoded, _ := json.Marshal(mm["content"])
		if len(encoded) > 0 && string(encoded) != "null" {
			return string(encoded)
		}
	}
	return ""
}

// SessionCache maps conversation seeds to fixed opencode-shaped session IDs so
// one conversation keeps prompt-cache affinity. The table is capped at 1000
// entries; exceeding it rebuilds the table, which merely rotates sessions.
type SessionCache struct {
	mu     sync.Mutex
	bySeed map[string]string
}

// NewSessionCache returns an empty session cache.
func NewSessionCache() *SessionCache {
	return &SessionCache{bySeed: map[string]string{}}
}

// Lookup returns the session ID for a seed, creating one on first use.
func (c *SessionCache) Lookup(seed []byte) string {
	key := sha256.Sum256(seed)
	keyStr := hex.EncodeToString(key[:8])
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bySeed == nil {
		c.bySeed = map[string]string{}
	}
	sess, ok := c.bySeed[keyStr]
	if !ok {
		sess = NewIdentifier().New("ses", true)
		if len(c.bySeed) >= 1000 {
			c.bySeed = map[string]string{}
		}
		c.bySeed[keyStr] = sess
	}
	return sess
}

// SessionID returns the upstream session for a request: a stable shaped
// session derived from the conversation seed, or a random one when the request
// carries no conversation signal.
func (c *SessionCache) SessionID(params map[string]any) string {
	if signal := ConversationSeed(params); signal != "" {
		return c.Lookup([]byte("ses\x00" + signal))
	}
	return NewIdentifier().New("ses", true)
}

// UserAgent returns the User-Agent opencode 1.19.0 sends through AI SDK for a
// protocol, whose provider-utils version differs per protocol.
func UserAgent(proto string) string {
	pu := ProviderUtilsVersions[proto]
	if pu == "" {
		pu = "4.0.23"
	}
	return fmt.Sprintf("opencode/%s ai-sdk/provider-utils/%s runtime/bun/1.3.14", ClientVersion, pu)
}
