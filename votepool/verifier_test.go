package votepool

import (
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/types"
)

func TestVoteFromValidatorVerifier(t *testing.T) {
	pubKey1 := ed25519.GenPrivKey().PubKey()
	blsPrivKey1, _ := bls.GenerateBlsKey()
	blsPubKey1 := blsPrivKey1.PublicKey().Marshal()
	val1 := &types.Validator{Address: pubKey1.Address(), PubKey: pubKey1, BlsKey: blsPubKey1, VotingPower: 10}

	pubKey2 := ed25519.GenPrivKey().PubKey()
	blsPrivKey2, _ := bls.GenerateBlsKey()
	blsPubKey2 := blsPrivKey2.PublicKey().Marshal()
	val2 := &types.Validator{Address: pubKey2.Address(), PubKey: pubKey2, BlsKey: blsPubKey2, VotingPower: 10}

	vals := make([]*types.Validator, 0)
	vals = append(vals, val1)
	vals = append(vals, val2)

	verifier := NewFromValidatorVerifier()
	verifier.initValidators(vals)

	voteFromVal1 := Vote{PubKey: blsPubKey1}
	err := verifier.Validate(&voteFromVal1)
	require.NoError(t, err)

	blsPrivKey, _ := bls.GenerateBlsKey()
	blsPubKey := blsPrivKey.PublicKey().Marshal()
	voteFromOthers := Vote{PubKey: blsPubKey}
	err = verifier.Validate(&voteFromOthers)
	require.Error(t, err)
}

func TestVoteFromValidatorVerifier_UpdateValidators(t *testing.T) {
	pubKey1 := ed25519.GenPrivKey().PubKey()
	blsPrivKey1, _ := bls.GenerateBlsKey()
	blsPubKey1 := blsPrivKey1.PublicKey().Marshal()
	val1 := &types.Validator{Address: pubKey1.Address(), PubKey: pubKey1, BlsKey: blsPubKey1, VotingPower: 10}

	pubKey2 := ed25519.GenPrivKey().PubKey()
	blsPrivKey2, _ := bls.GenerateBlsKey()
	blsPubKey2 := blsPrivKey2.PublicKey().Marshal()
	val2 := &types.Validator{Address: pubKey2.Address(), PubKey: pubKey2, BlsKey: blsPubKey2, VotingPower: 10}

	vals := make([]*types.Validator, 0)
	vals = append(vals, val1)
	vals = append(vals, val2)

	verifier := NewFromValidatorVerifier()
	verifier.initValidators(vals)

	//remove validator
	removeVal := &types.Validator{PubKey: pubKey1, Address: pubKey1.Address(), VotingPower: 0}
	require.NoError(t, verifier.updateValidators([]*types.Validator{removeVal}))

	require.Equal(t, 1, len(verifier.validators))

	//add validator
	pubKey3 := ed25519.GenPrivKey().PubKey()
	blsPrivKey3, _ := bls.GenerateBlsKey()
	blsPubKey3 := blsPrivKey3.PublicKey().Marshal()

	addVal := &types.Validator{PubKey: pubKey3, Address: pubKey3.Address(), BlsKey: blsPubKey3, VotingPower: 10}
	require.NoError(t, verifier.updateValidators([]*types.Validator{addVal}))

	require.Equal(t, 2, len(verifier.validators))
}

func TestVoteBlsVerifier(t *testing.T) {
	privKey, _ := bls.GenerateBlsKey()
	pubKey := privKey.PublicKey().Marshal()
	eventHash := common.HexToHash("0xeefacfed87736ae1d8e8640f6fd7951862997782e5e79842557923e2779d5d5a").Bytes()
	privKeyBts, _ := privKey.Marshal()
	secKey, _ := bls.UnmarshalPrivateKey(privKeyBts)
	signBts := signVote(secKey, EventType(0), eventHash)

	verifier := &BlsSignatureVerifier{}

	vote1 := Vote{
		PubKey:    pubKey,
		Signature: signBts,
		EventType: 0,
		EventHash: eventHash,
		expireAt:  time.Time{},
	}
	err := verifier.Validate(&vote1)
	require.NoError(t, err)

	vote2 := Vote{
		PubKey:    pubKey,
		Signature: signBts,
		EventType: 0,
		EventHash: common.HexToHash("0xb3989c2ba4b4b91b35162c137c154848f7261e16ce3f6d8c88f64cf06b737a3c").Bytes(),
		expireAt:  time.Time{},
	}
	err = verifier.Validate(&vote2)
	require.Error(t, err)
}

// TestBlsSignatureVerifier_RejectsCrossEventTypeReplay pins the EventType binding
// in the signed preimage. A signature produced for one event type must not verify
// when the same (EventHash, PubKey) is replayed under a different event type.
//
// Before EventType was bound into the preimage this replay verified successfully,
// which let an attacker inject a valid vote into the wrong bucket and — because
// the dedup key was also EventType-blind — censor the legitimate vote for the
// original bucket.
func TestBlsSignatureVerifier_RejectsCrossEventTypeReplay(t *testing.T) {
	blsPrivKey, err := bls.GenerateBlsKey()
	require.NoError(t, err)
	privBts, err := blsPrivKey.Marshal()
	require.NoError(t, err)
	secKey, err := bls.UnmarshalPrivateKey(privBts)
	require.NoError(t, err)

	eventHash := common.HexToHash("0xeefacfed87736ae1d8e8640f6fd7951862997782e5e79842557923e2779d5d5a").Bytes()
	signed := signVote(secKey, FromBscCrossChainEvent, eventHash)

	verifier := &BlsSignatureVerifier{}

	// Same event type it was signed for: valid.
	honest := &Vote{
		PubKey:    blsPrivKey.PublicKey().Marshal(),
		Signature: signed,
		EventType: FromBscCrossChainEvent,
		EventHash: eventHash,
	}
	require.NoError(t, verifier.Validate(honest), "vote must verify under the event type it was signed for")

	// Replayed into a different bucket with the identical signature: rejected.
	for _, replayed := range []EventType{
		ToBscCrossChainEvent,
		FromOpCrossChainEvent,
		ToOpCrossChainEvent,
		DataAvailabilityChallengeEvent,
	} {
		v := &Vote{
			PubKey:    blsPrivKey.PublicKey().Marshal(),
			Signature: signed,
			EventType: replayed,
			EventHash: eventHash,
		}
		require.Error(t, verifier.Validate(v),
			"signature for FromBscCrossChainEvent must not verify as event type %d", replayed)
	}

	// The dedup key must also differ per event type, otherwise a vote cached under
	// one type silently suppresses the legitimate vote for another.
	require.NotEqual(t, honest.Key(), (&Vote{
		PubKey:    honest.PubKey,
		Signature: honest.Signature,
		EventType: ToBscCrossChainEvent,
		EventHash: eventHash,
	}).Key(), "dedup key must be event-type specific")
}
