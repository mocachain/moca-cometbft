package core

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/votepool"
)

// TestTokenBucket_BurstThenDeny: hands out exactly `burst` tokens, then denies.
func TestTokenBucket_BurstThenDeny(t *testing.T) {
	b := newTokenBucket(0, 3) // no refill, burst 3
	for i := 0; i < 3; i++ {
		require.True(t, b.allow(), "burst token %d must be granted", i)
	}
	require.False(t, b.allow(), "call past the burst must be denied without refill")
}

// TestBroadcastVote_RateLimited: with the limiter exhausted the handler rejects
// before touching VotePool (nil here). Fails against the pre-change handler.
func TestBroadcastVote_RateLimited(t *testing.T) {
	saved := voteBroadcastLimiter
	defer func() { voteBroadcastLimiter = saved }()
	voteBroadcastLimiter = newTokenBucket(0, 0) // deny everything

	env := &Environment{} // VotePool intentionally nil: must be rejected before use
	_, err := env.BroadcastVote(nil, votepool.Vote{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit")
}
