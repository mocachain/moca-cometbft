package votepool

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-kit/log/term"
	"github.com/stretchr/testify/require"

	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/p2p"
	votepoolproto "github.com/cometbft/cometbft/proto/tendermint/votepool"
	"github.com/cometbft/cometbft/types"
)

const testEventType = FromBscCrossChainEvent

// votepoolLogger is a TestingLogger which uses a different
// color for each validator ("validator" key must exist).
func votepoolLogger() log.Logger {
	return log.TestingLoggerWithColorFn(func(keyvals ...interface{}) term.FgBgColor {
		for i := 0; i < len(keyvals)-1; i += 2 {
			if keyvals[i] == "validator" {
				return term.FgBgColor{Fg: term.Color(uint8(keyvals[i+1].(int) + 1))}
			}
		}
		return term.FgBgColor{}
	})
}

func makeAndConnectReactors(config *cfg.Config, n int) ([]*bls.PrivateKey, []*types.Validator, []*types.EventBus, []VotePool, []*Reactor) {
	pubKey1 := ed25519.GenPrivKey().PubKey()
	blsPrivKey1, _ := bls.GenerateBlsKey()
	blsPubKey1 := blsPrivKey1.PublicKey().Marshal()
	val1 := &types.Validator{Address: pubKey1.Address(), PubKey: pubKey1, BlsKey: blsPubKey1, VotingPower: 10}

	pubKey2 := ed25519.GenPrivKey().PubKey()
	blsPrivKey2, _ := bls.GenerateBlsKey()
	blsPubKey2 := blsPrivKey2.PublicKey().Marshal()
	val2 := &types.Validator{Address: pubKey2.Address(), PubKey: pubKey2, BlsKey: blsPubKey2, VotingPower: 10}

	pks := []*bls.PrivateKey{
		blsPrivKey1, blsPrivKey2,
	}

	vals := []*types.Validator{
		val1, val2,
	}

	eventBuses := make([]*types.EventBus, n)
	votePools := make([]VotePool, n)
	reactors := make([]*Reactor, n)

	logger := votepoolLogger()
	for i := 0; i < n; i++ {
		eventBus := types.NewEventBus()
		err := eventBus.Start()
		if err != nil {
			panic(err)
		}

		votePool := NewVotePool(logger, vals, eventBus)

		eventBuses[i] = eventBus
		votePools[i] = votePool
		reactors[i] = NewReactor(votePool, eventBus)
		reactors[i].SetLogger(logger.With("validator", i))
	}

	p2p.MakeConnectedSwitches(config.P2P, n, func(i int, s *p2p.Switch) *p2p.Switch {
		s.AddReactor("VOTEPOOL", reactors[i])
		return s

	}, p2p.Connect2Switches)
	return pks, vals, eventBuses, votePools, reactors
}

func TestReactorBroadcastVotes(t *testing.T) {
	config := cfg.TestConfig()
	pks, vals, _, pools, reactors := makeAndConnectReactors(config, 2)

	pks0Bts, _ := pks[0].Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pks0Bts)
	eventHash1 := common.HexToHash("0xeefacfed87736ae1d8e8640f6fd7951862997782e5e79842557923e2779d5d5a").Bytes()
	sign1Bts := signVote(secKey, testEventType, eventHash1)
	vote1 := Vote{
		PubKey:    vals[0].BlsKey,
		Signature: sign1Bts,
		EventType: testEventType,
		EventHash: eventHash1,
	}
	err := pools[0].AddVote(&vote1)
	require.NoError(t, err)

	waitVotesReceived(t, reactors, eventHash1)

	eventHash2 := common.HexToHash("0x7e19be15d0d524a1ca5e39be503d18584c23426920bdc23b159c37a2341913d0").Bytes()
	sign2Bts := signVote(secKey, testEventType, eventHash2)
	vote2 := Vote{
		PubKey:    vals[0].BlsKey,
		Signature: sign2Bts,
		EventType: testEventType,
		EventHash: eventHash2,
	}
	err = pools[0].AddVote(&vote2)
	require.NoError(t, err)

	waitVotesReceived(t, reactors, eventHash2)
}

func waitVotesReceived(t *testing.T, reactors []*Reactor, eventHash []byte) {
	wg := new(sync.WaitGroup)
	for i, reactor := range reactors {
		wg.Add(1)
		go func(r *Reactor, reactorIndex int) {
			defer wg.Done()
			waitForVoteOnReactor(t, eventHash, r)
		}(reactor, i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	timer := time.After(20 * time.Second)
	select {
	case <-timer:
		t.Fatal("Timed out waiting for vote")
	case <-done:
	}
}

func waitForVoteOnReactor(t *testing.T, eventHash []byte, r *Reactor) {
	for {
		time.Sleep(time.Millisecond * 100)
		votes, _ := r.votePool.GetVotesByEventType(testEventType)
		found := false
		for _, vote := range votes {
			if bytes.Equal(eventHash, vote.EventHash) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
}

// TestReactorResubscribesAfterCancel: after a peer's subscription is canceled,
// a later vote must still reach the peer (the reactor resubscribes rather than
// exiting). Fails against the pre-change reactor (times out on the second vote).
func TestReactorResubscribesAfterCancel(t *testing.T) {
	config := cfg.TestConfig()
	pks, vals, eventBuses, pools, reactors := makeAndConnectReactors(config, 2)

	pks0Bts, _ := pks[0].Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pks0Bts)
	mkVote := func(hashHex string) Vote {
		h := common.HexToHash(hashHex).Bytes()
		return Vote{PubKey: vals[0].BlsKey, Signature: signVote(secKey, testEventType, h), EventType: testEventType, EventHash: h}
	}

	// 1) gossip works initially: reactor[1] receives reactor[0]'s vote.
	v1 := mkVote("0x1111111111111111111111111111111111111111111111111111111111111111")
	require.NoError(t, pools[0].AddVote(&v1))
	waitVotesReceived(t, reactors, v1.EventHash)

	// 2) cancel reactor[0]'s subscription for its peer (as a buffer overflow would).
	peers := reactors[0].Switch.Peers().List()
	require.NotEmpty(t, peers)
	require.NoError(t, eventBuses[0].Unsubscribe(context.Background(), string(peers[0].ID()), eventVotePoolAdded))

	// 3) after the resubscribe backoff, a new vote must still reach reactor[1].
	time.Sleep(resubscribeBackoff + 500*time.Millisecond)
	v2 := mkVote("0x2222222222222222222222222222222222222222222222222222222222222222")
	require.NoError(t, pools[0].AddVote(&v2))
	waitVotesReceived(t, reactors, v2.EventHash)
}

// receiveVote hands msg to the reactor as if it had arrived from src.
func receiveVote(r *Reactor, src p2p.Peer, v Vote) {
	r.Receive(p2p.Envelope{
		ChannelID: VotePoolChannel,
		Src:       src,
		Message: &votepoolproto.Vote{
			PubKey:    v.PubKey,
			Signature: v.Signature,
			EventType: uint32(v.EventType),
			EventHash: v.EventHash,
		},
	})
}

// TestReactorStopsPeerOverInvalidVoteBudget: every vote that fails verification
// costs a BLS pairing, and the negative cache cannot absorb a stream of them --
// each carries a fresh signature, so it never hits. A peer that spends its whole
// budget inside the window is disconnected.
func TestReactorStopsPeerOverInvalidVoteBudget(t *testing.T) {
	config := cfg.TestConfig()
	_, vals, _, _, reactors := makeAndConnectReactors(config, 2)

	peers := reactors[0].Switch.Peers().List()
	require.Len(t, peers, 1)
	src := peers[0]

	eventHash := common.HexToHash("0x2b7c4e1a8d5f0c3b6e9a2d5f8c1b4e7a0d3f6c9b2e5a8d1f4c7b0e3a6d9f2c5b").Bytes()
	badVote := func(n int) Vote {
		// A fresh signature every time, so the negative cache never short-circuits.
		sig := make([]byte, signatureLen)
		binary.BigEndian.PutUint64(sig, uint64(n)+1)
		return Vote{PubKey: vals[0].BlsKey, Signature: sig, EventType: testEventType, EventHash: eventHash}
	}

	for i := 0; i < maxInvalidVotesPerPeer; i++ {
		receiveVote(reactors[0], src, badVote(i))
	}
	require.Equal(t, 1, reactors[0].Switch.Peers().Size(),
		"the peer must not be dropped while still inside its budget")

	receiveVote(reactors[0], src, badVote(maxInvalidVotesPerPeer))
	require.Eventually(t, func() bool { return reactors[0].Switch.Peers().Size() == 0 },
		5*time.Second, 50*time.Millisecond,
		"peer was never stopped after exceeding its invalid-vote budget")
}

// TestReactorKeepsPeerSendingBenignVotes: accepted votes, duplicates of them and
// replays of an already-rejected signature are all free to detect, so none of
// them may spend the budget.
func TestReactorKeepsPeerSendingBenignVotes(t *testing.T) {
	config := cfg.TestConfig()
	pks, vals, _, _, reactors := makeAndConnectReactors(config, 2)

	peers := reactors[0].Switch.Peers().List()
	require.Len(t, peers, 1)
	src := peers[0]

	pks0Bts, _ := pks[0].Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pks0Bts)
	eventHash := common.HexToHash("0x6f1b8c3e5a7d0f2b4c6e8a1d3f5b7c9e0a2d4f6b8c1e3a5d7f9b0c2e4a6d8f1b").Bytes()

	good := Vote{
		PubKey:    vals[0].BlsKey,
		Signature: signVote(secKey, testEventType, eventHash),
		EventType: testEventType,
		EventHash: eventHash,
	}
	// One accepted vote, then the same vote over and over: every repeat is a
	// duplicate the pool answers from its cache.
	for i := 0; i <= maxInvalidVotesPerPeer; i++ {
		receiveVote(reactors[0], src, good)
	}

	// The same rejected signature over and over: the first costs a pairing, the
	// rest are answered from the negative cache.
	otherHash := common.HexToHash("0xc4a7f0d3b6e9a2c5f8b1d4e7a0c3f6b9d2e5a8c1f4b7d0e3a6c9f2b5d8e1a4c7").Bytes()
	bad := Vote{
		PubKey:    vals[0].BlsKey,
		Signature: make([]byte, signatureLen),
		EventType: testEventType,
		EventHash: otherHash,
	}
	for i := 0; i <= maxInvalidVotesPerPeer; i++ {
		receiveVote(reactors[0], src, bad)
	}

	require.Equal(t, 1, reactors[0].Switch.Peers().Size(),
		"duplicates and repeats of a known-bad signature must not drop the peer")
}

// TestVoteBudget_WindowResets pins the budget as a rate, not a lifetime total: a
// peer with an occasional failure never accumulates its way to a disconnect.
func TestVoteBudget_WindowResets(t *testing.T) {
	budget := &voteBudget{}
	start := time.Now()

	for i := 0; i < maxInvalidVotesPerPeer; i++ {
		require.False(t, budget.spend(start), "failure %d is still inside the budget", i)
	}
	require.True(t, budget.spend(start), "one past the budget must report over")

	require.False(t, budget.spend(start.Add(invalidVoteWindow+time.Second)),
		"the count must start again in a new window")
}

// TestReactorKeepsPeerSendingCheapRejections: a rejection settled by a length
// check or a map lookup never reaches the signature verifier, and an honest peer
// hits one whenever its view of the event types or the validator set is a step
// ahead of ours. Neither may spend the budget.
func TestReactorKeepsPeerSendingCheapRejections(t *testing.T) {
	config := cfg.TestConfig()
	_, vals, _, _, reactors := makeAndConnectReactors(config, 2)

	peers := reactors[0].Switch.Peers().List()
	require.Len(t, peers, 1)
	src := peers[0]

	eventHash := common.HexToHash("0x0e5b8a2d7f1c4b9e6a3d0f7c2b5e8a1d4f7c0b3e6a9d2f5c8b1e4a7d0f3c6b9e").Bytes()
	freshSig := func(n int) []byte {
		sig := make([]byte, signatureLen)
		binary.BigEndian.PutUint64(sig, uint64(n)+1)
		return sig
	}

	// An event type this node does not know, as a peer one upgrade ahead would
	// gossip: rejected by ValidateBasic.
	unknownType := func(n int) Vote {
		return Vote{PubKey: vals[0].BlsKey, Signature: freshSig(n), EventType: EventType(9), EventHash: eventHash}
	}
	// A key that is not a current validator, as validator-set skew produces:
	// rejected by a map lookup.
	strangerKey, _ := bls.GenerateBlsKey()
	unknownValidator := func(n int) Vote {
		return Vote{
			PubKey: strangerKey.PublicKey().Marshal(), Signature: freshSig(n),
			EventType: testEventType, EventHash: eventHash,
		}
	}

	for i := 0; i <= maxInvalidVotesPerPeer; i++ {
		receiveVote(reactors[0], src, unknownType(i))
		receiveVote(reactors[0], src, unknownValidator(i))
	}

	require.Equal(t, 1, reactors[0].Switch.Peers().Size(),
		"rejections that never reach the signature verifier must not drop the peer")
}
