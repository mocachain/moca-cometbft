package core

import (
	"errors"
	"sync"
	"time"

	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	"github.com/cometbft/cometbft/votepool"
)

// voteBroadcastLimiter caps how fast the public broadcast_vote endpoint reaches
// BLS verification, so an unauthenticated client cannot force unbounded BN254
// pairings. The route stays public (the DA challenger needs it); the limit is far
// above any honest rate, and p2p gossip is untouched.
var voteBroadcastLimiter = newTokenBucket(50, 100) // 50/s sustained, burst 100

func (env *Environment) BroadcastVote(ctx *rpctypes.Context, vote votepool.Vote) (*ctypes.ResultBroadcastVote, error) {
	if !voteBroadcastLimiter.allow() {
		return &ctypes.ResultBroadcastVote{}, errors.New("broadcast_vote rate limit exceeded")
	}
	err := env.VotePool.AddVote(&vote)
	return &ctypes.ResultBroadcastVote{}, err
}

// tokenBucket is a minimal, dependency-free rate limiter. It lives in the RPC
// layer (not the state machine), so wall-clock time is fine here.
type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens refilled per second
	last   time.Time
}

func newTokenBucket(ratePerSec, burst float64) *tokenBucket {
	return &tokenBucket{tokens: burst, max: burst, rate: ratePerSec, last: time.Now()}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (env *Environment) QueryVote(ctx *rpctypes.Context, eventType int, eventHash []byte) (*ctypes.ResultQueryVote, error) {
	var votes []*votepool.Vote
	var err error
	if len(eventHash) == 0 {
		votes, err = env.VotePool.GetVotesByEventType(votepool.EventType(eventType))
	} else {
		votes, err = env.VotePool.GetVotesByEventTypeAndHash(votepool.EventType(eventType), eventHash)
	}

	return &ctypes.ResultQueryVote{Votes: votes}, err
}

func (env *Environment) UnsafeFlushVotePool(ctx *rpctypes.Context) (*ctypes.ResultFlushVote, error) {
	env.VotePool.FlushVotes()
	return &ctypes.ResultFlushVote{}, nil
}
