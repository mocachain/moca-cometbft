package votepool

import (
	"context"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/libs/log"

	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/types"
)

func makeVotePool() (*bls.PrivateKey, *types.Validator, *bls.PrivateKey, *types.Validator, *types.EventBus, *Pool) {
	pubKey1 := ed25519.GenPrivKey().PubKey()
	blsPrivKey1, _ := bls.GenerateBlsKey()
	blsPubKey1 := blsPrivKey1.PublicKey().Marshal()
	val1 := &types.Validator{Address: pubKey1.Address(), PubKey: pubKey1, BlsKey: blsPubKey1, VotingPower: 10}

	pubKey2 := ed25519.GenPrivKey().PubKey()
	blsPrivKey2, _ := bls.GenerateBlsKey()
	blsPubKey2 := blsPrivKey2.PublicKey().Marshal()
	val2 := &types.Validator{Address: pubKey2.Address(), PubKey: pubKey2, BlsKey: blsPubKey2, VotingPower: 10}

	vals := []*types.Validator{
		val1, val2,
	}

	logger := log.TestingLogger()
	eventBus := types.NewEventBus()
	err := eventBus.Start()
	if err != nil {
		panic(err)
	}

	votePool := NewVotePool(logger, vals, eventBus)
	err = votePool.Start()
	if err != nil {
		panic(err)
	}

	return blsPrivKey1, val1, blsPrivKey2, val2, eventBus, votePool
}

func makeValidVotes(secKey *bls.PrivateKey, val1 *types.Validator) (Vote, Vote, Vote) {
	eventHash1 := common.HexToHash("0xeefacfed87736ae1d8e8640f6fd7951862997782e5e79842557923e2779d5d5a").Bytes()
	sign1, _ := secKey.Sign(eventHash1, DST)
	sign1Bts, _ := sign1.Marshal()
	vote1 := Vote{
		PubKey:    val1.BlsKey,
		Signature: sign1Bts,
		EventType: FromBscCrossChainEvent,
		EventHash: eventHash1,
	}

	eventHash2 := common.HexToHash("0x7e19be15d0d524a1ca5e39be503d18584c23426920bdc23b159c37a2341913d0").Bytes()
	sign2, _ := secKey.Sign(eventHash2, DST)
	sign2Bts, _ := sign2.Marshal()
	vote2 := Vote{
		PubKey:    val1.BlsKey,
		Signature: sign2Bts,
		EventType: ToBscCrossChainEvent,
		EventHash: eventHash2,
	}

	eventHash3 := common.HexToHash("0xb941130c8d3508f642aba83db420f9cef6a6ebb7f869e3cef06f276bdcf205a9").Bytes()
	sign3, _ := secKey.Sign(eventHash3, DST)
	sign3Bts, _ := sign3.Marshal()
	vote3 := Vote{
		PubKey:    val1.BlsKey,
		Signature: sign3Bts,
		EventType: FromBscCrossChainEvent,
		EventHash: eventHash3,
	}
	return vote1, vote2, vote3
}

// removeValidatorUpdate builds an update that drops val from the validator set.
func removeValidatorUpdate(val *types.Validator) types.EventDataValidatorSetUpdates {
	return types.EventDataValidatorSetUpdates{
		ValidatorUpdates: []*types.Validator{
			{PubKey: val.PubKey, Address: val.Address, VotingPower: 0},
		},
	}
}

// addValidatorUpdate builds an update that puts val back into the validator set.
func addValidatorUpdate(val *types.Validator) types.EventDataValidatorSetUpdates {
	return types.EventDataValidatorSetUpdates{
		ValidatorUpdates: []*types.Validator{
			{PubKey: val.PubKey, Address: val.Address, BlsKey: val.BlsKey, VotingPower: 10},
		},
	}
}

// requireValidatorCount republishes update until the pool's validator set
// reaches want. Republishing matters because the pool may be inside its
// resubscribe backoff, during which published events reach nobody; bounding it
// matters because a pool that never applies the update should fail the test
// rather than hang it.
func requireValidatorCount(t *testing.T, p *Pool, bus *types.EventBus, update types.EventDataValidatorSetUpdates, want int, msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_ = bus.PublishEventValidatorSetUpdates(update)
		return p.validatorVerifier.lenOfValidators() == want
	}, 15*time.Second, 200*time.Millisecond, msg)
}

func TestPool_AddVote(t *testing.T) {
	pk1, val1, _, _, _, votePool := makeVotePool()

	eventHash := common.HexToHash("0xeefacfed87736ae1d8e8640f6fd7951862997782e5e79842557923e2779d5d5a").Bytes()
	pk1Bts, _ := pk1.Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pk1Bts)
	sign, _ := secKey.Sign(eventHash, DST)
	signBts, _ := sign.Marshal()

	anotherEventHash := common.HexToHash("0x7e19be15d0d524a1ca5e39be503d18584c23426920bdc23b159c37a2341913d0").Bytes()
	blsPrivKey, _ := bls.GenerateBlsKey()
	blsPubKey := blsPrivKey.PublicKey().Marshal()
	blsPrivKeyBts, _ := blsPrivKey.Marshal()
	blsSecKey, _ := bls.UnmarshalPrivateKey(blsPrivKeyBts)
	anotherSign, _ := blsSecKey.Sign(anotherEventHash, DST)
	anotherSignBts, _ := anotherSign.Marshal()

	testCases := []struct {
		vote Vote
		err  bool
		msg  string
	}{
		{
			vote: Vote{
				PubKey:    val1.BlsKey,
				Signature: signBts,
				EventType: FromBscCrossChainEvent,
				EventHash: eventHash,
			},
			err: false,
			msg: "vote can be added",
		},
		{
			vote: Vote{
				PubKey:    val1.BlsKey,
				Signature: signBts,
				EventType: FromBscCrossChainEvent,
				EventHash: eventHash,
			},
			err: false,
			msg: "vote can be re-added even it is not re-stored",
		},
		{
			vote: Vote{
				PubKey:    blsPubKey,
				Signature: anotherSignBts,
				EventType: FromBscCrossChainEvent,
				EventHash: anotherEventHash,
			},
			err: true,
			msg: "vote is not from validators",
		},
		{
			vote: Vote{
				PubKey:    val1.BlsKey,
				Signature: anotherSignBts,
				EventType: FromBscCrossChainEvent,
				EventHash: anotherEventHash,
			},
			err: true,
			msg: "invalid signature",
		},
	}

	for _, tc := range testCases {
		err := votePool.AddVote(&tc.vote)
		if tc.err {
			if assert.Error(t, err) {
				assert.Equal(t, tc.msg, err.Error())
			}
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestPool_QueryFlushVote(t *testing.T) {
	pk1, val1, _, _, _, votePool := makeVotePool()
	pk1Bts, _ := pk1.Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pk1Bts)

	vote1, vote2, vote3 := makeValidVotes(secKey, val1)

	err := votePool.AddVote(&vote1)
	require.NoError(t, err)
	err = votePool.AddVote(&vote2)
	require.NoError(t, err)
	err = votePool.AddVote(&vote3)
	require.NoError(t, err)

	result, err := votePool.GetVotesByEventType(FromBscCrossChainEvent)
	require.NoError(t, err)
	require.Equal(t, 2, len(result))
	result, err = votePool.GetVotesByEventType(ToBscCrossChainEvent)
	require.NoError(t, err)
	require.Equal(t, 1, len(result))

	result, err = votePool.GetVotesByEventTypeAndHash(vote1.EventType, vote1.EventHash)
	require.NoError(t, err)
	require.Equal(t, 1, len(result))
	require.Equal(t, vote1.EventHash, result[0].EventHash)

	// cannot find
	result, err = votePool.GetVotesByEventTypeAndHash(ToBscCrossChainEvent, vote1.EventHash)
	require.NoError(t, err)
	require.Equal(t, 0, len(result))

	// flush
	votePool.FlushVotes()

	result, err = votePool.GetVotesByEventType(FromBscCrossChainEvent)
	require.NoError(t, err)
	require.Equal(t, 0, len(result))
	result, err = votePool.GetVotesByEventType(ToBscCrossChainEvent)
	require.NoError(t, err)
	require.Equal(t, 0, len(result))
}

func TestPool_PruneVote(t *testing.T) {
	pk1, val1, _, _, _, votePool := makeVotePool()
	pk1Bts, _ := pk1.Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pk1Bts)

	vote1, vote2, _ := makeValidVotes(secKey, val1)

	err := votePool.AddVote(&vote1)
	require.NoError(t, err)
	err = votePool.AddVote(&vote2)
	require.NoError(t, err)

	time.Sleep(voteKeepAliveAfter)
	time.Sleep(pruneVoteInterval)

	result, err := votePool.GetVotesByEventType(FromBscCrossChainEvent)
	require.NoError(t, err)
	require.Equal(t, 0, len(result))
	result, err = votePool.GetVotesByEventType(ToBscCrossChainEvent)
	require.NoError(t, err)
	require.Equal(t, 0, len(result))
}

func TestPool_SubscribeNewVoteEvent(t *testing.T) {
	pk1, val1, _, _, eventBus, votePool := makeVotePool()
	pk1Bts, _ := pk1.Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pk1Bts)

	vote1, _, _ := makeValidVotes(secKey, val1)

	sub, err := eventBus.Subscribe(context.Background(), "VotePoolUpdateSubscriber", eventVotePoolAdded, eventBusSubscribeCap)
	require.NoError(t, err)

	err = votePool.AddVote(&vote1)
	require.NoError(t, err)

	select {
	case msg := <-sub.Out():
		event, ok := msg.Data().(Vote)
		require.True(t, ok, "Expected event of type Vote, got %T", msg.Data())
		require.Equal(t, vote1.EventHash, event.EventHash)
	case <-sub.Canceled():
		t.Fatalf("sub was canceled (reason: %v)", sub.Err())
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive EventVotePoolUpdates within 1 sec.")
	}
}

func TestPool_ValidatorSetUpdate(t *testing.T) {
	pk1, val1, _, _, eventBus, votePool := makeVotePool()
	pk1Bts, _ := pk1.Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pk1Bts)

	// remove validator 1; resending the same update is expected to be harmless
	requireValidatorCount(t, votePool, eventBus, removeValidatorUpdate(val1), 1,
		"validator 1 was never removed from the set")

	vote1, _, _ := makeValidVotes(secKey, val1)
	err := votePool.AddVote(&vote1)
	require.Error(t, err, "vote is not from validators")

	// add validator 1 back
	requireValidatorCount(t, votePool, eventBus, addValidatorUpdate(val1), 2,
		"validator 1 was never added back to the set")

	err = votePool.AddVote(&vote1)
	require.NoError(t, err)
}

// TestPool_NegativeCacheSkipsRepeatBlsVerification pins that a (vote, signature)
// pair which already failed BLS verification is rejected on resubmission without
// re-running the pairing, and that a later vote carrying a *correct* signature for
// the same event and key is still accepted.
func TestPool_NegativeCacheSkipsRepeatBlsVerification(t *testing.T) {
	pk1, val1, _, _, _, votePool := makeVotePool()
	pk1Bts, _ := pk1.Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(pk1Bts)

	eventHash := common.HexToHash("0x1d1e6a8a4e3b7f9c2d5e8a0b3c6d9f2a5b8e1c4d7a0b3e6f9c2d5a8b1e4f7a0b").Bytes()

	// A real validator key with a bogus signature: passes the cheap validator
	// check and would otherwise reach BLS verification on every submission.
	bad := &Vote{
		PubKey:    val1.BlsKey,
		Signature: make([]byte, signatureLen),
		EventType: FromBscCrossChainEvent,
		EventHash: eventHash,
	}
	require.Error(t, votePool.AddVote(bad), "bogus signature must be rejected")
	negKey := bad.Key() + string(bad.Signature)
	require.True(t, votePool.negCache.Contains(negKey), "failed pair must be remembered")

	// Same bad pair again: still rejected, now short-circuited by the cache.
	require.Error(t, votePool.AddVote(bad), "repeat of a known-bad signature must stay rejected")

	// A correct signature for the same event and key must still get through --
	// the negative cache keys on the signature, not just the vote identity.
	good := &Vote{
		PubKey:    val1.BlsKey,
		Signature: func() []byte { sig, _ := secKey.Sign(eventHash, DST); b, _ := sig.Marshal(); return b }(),
		EventType: FromBscCrossChainEvent,
		EventHash: eventHash,
	}
	require.NoError(t, votePool.AddVote(good), "valid signature must not be blocked by the negative cache")
}

// TestVoteStore_PruneKeepsReinsertedVote pins the defect this fix is named for.
//
// The same (EventHash, PubKey) can be inserted twice: once the dedup cache
// evicts its key, a resubmission reaches addVote again, overwrites the map entry
// and appends a SECOND queue entry. When the older queue entry expires, pruning
// by (EventHash, PubKey) alone deleted the newer, still-live vote.
func TestVoteStore_PruneKeepsReinsertedVote(t *testing.T) {
	store := newVoteStore()

	eventHash := common.HexToHash("0x3f2a1b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a").Bytes()
	pubKey := make([]byte, pubKeyLen)

	// First insertion, already expired.
	stale := &Vote{PubKey: pubKey, EventType: FromBscCrossChainEvent, EventHash: eventHash}
	stale.expireAt = time.Now().Add(-time.Minute)
	store.addVote(stale)

	// Re-insertion of the same identity while the stale queue entry is still
	// queued -- this is what happens after a dedup-cache eviction.
	fresh := &Vote{PubKey: pubKey, EventType: FromBscCrossChainEvent, EventHash: eventHash}
	fresh.expireAt = time.Now().Add(time.Minute)
	store.addVote(fresh)

	store.pruneVotes()

	got := store.getVotesByEventHash(eventHash)
	require.Len(t, got, 1, "the live re-inserted vote must survive pruning of the stale entry")
	require.Same(t, fresh, got[0], "the surviving vote must be the re-inserted one, not the expired one")
// A canceled subscription must be recovered from. pubsub cancels the whole
// subscription when the subscriber's buffer overflows; returning at that point
// freezes the validator set for the lifetime of the process, so removed
// validators keep being accepted and newly added ones keep being rejected.
func TestPool_ResubscribesAfterSubscriptionCanceled(t *testing.T) {
	_, val1, _, _, eventBus, votePool := makeVotePool()
	t.Cleanup(func() { _ = votePool.Stop(); _ = eventBus.Stop() })

	require.Equal(t, 2, votePool.validatorVerifier.lenOfValidators())

	// Cancel the subscription out from under the pool, as an overflow would.
	// Retried because the routine subscribes asynchronously after Start; a
	// successful Unsubscribe is also proof the subscription was there to cancel.
	require.Eventually(t, func() bool {
		return eventBus.Unsubscribe(
			context.Background(), votePoolSubscriber, types.EventQueryValidatorSetUpdates) == nil
	}, 5*time.Second, 50*time.Millisecond, "pool never subscribed to validator updates")

	requireValidatorCount(t, votePool, eventBus, removeValidatorUpdate(val1), 1,
		"validator updates stopped being applied after the subscription was canceled")
}

// Stopping the pool must release its event-bus client. If it does not, a pool
// restarted on the same bus re-subscribes with the same ID, gets
// ErrAlreadySubscribed, and silently never receives another validator update.
func TestPool_RestartStillReceivesValidatorUpdates(t *testing.T) {
	_, val1, _, val2, eventBus, first := makeVotePool()
	t.Cleanup(func() { _ = eventBus.Stop() })

	// Wait until the first pool is demonstrably subscribed and consuming. Without
	// this it is usually stopped before it ever registered its client, so the
	// leak under test never gets a chance to happen and the assertion below
	// passes for the wrong reason.
	requireValidatorCount(t, first, eventBus, removeValidatorUpdate(val2), 1,
		"first pool never applied a validator update, so it was never subscribed")

	require.NoError(t, first.Stop())

	second := NewVotePool(log.TestingLogger(), []*types.Validator{val1, val2}, eventBus)
	require.NoError(t, second.Start())
	t.Cleanup(func() { _ = second.Stop() })

	requireValidatorCount(t, second, eventBus, removeValidatorUpdate(val1), 1,
		"restarted pool never applied a validator update -- the previous pool's subscription was not released")
}

// A payload of an unexpected type published under the validator-update query
// must not take the routine down with it.
func TestPool_UnexpectedValidatorUpdatePayloadDoesNotStopUpdates(t *testing.T) {
	_, val1, _, _, eventBus, votePool := makeVotePool()
	t.Cleanup(func() { _ = votePool.Stop(); _ = eventBus.Stop() })

	// Wait until the routine is demonstrably subscribed and consuming. Published
	// before that, the bad payload is simply never delivered and the test passes
	// without ever exercising the assertion it exists for.
	requireValidatorCount(t, votePool, eventBus, removeValidatorUpdate(val1), 1,
		"pool never applied a validator update, so it was never subscribed")

	require.NoError(t, eventBus.Publish(
		types.EventValidatorSetUpdates, types.EventDataString("unexpected payload")))

	requireValidatorCount(t, votePool, eventBus, addValidatorUpdate(val1), 2,
		"validator updates stopped after an unexpected payload was published")
}
