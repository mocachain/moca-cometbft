package votepool

import (
	"errors"

	"github.com/0xPolygon/polygon-edge/bls"

	"github.com/cometbft/cometbft/libs/sync"
	"github.com/cometbft/cometbft/types"
)

// Verifier will validate Votes by different policies.
type Verifier interface {
	Validate(vote *Vote) error
}

// FromValidatorVerifier will check whether the Vote is from a valid validator.
//
// It tracks the FULL validator set, not just the members holding a BLS key: an
// update removing a BLS-less validator has to resolve against the same set the
// node has, or UpdateWithChangeSet rejects the whole batch and the additions in
// it are lost. blsKeys is the derived index used for the vote source check.
type FromValidatorVerifier struct {
	mtx        *sync.RWMutex
	validators *types.ValidatorSet
	blsKeys    map[string]*types.Validator
}

func NewFromValidatorVerifier() *FromValidatorVerifier {
	f := &FromValidatorVerifier{
		validators: &types.ValidatorSet{},
		blsKeys:    make(map[string]*types.Validator),
		mtx:        &sync.RWMutex{},
	}
	return f
}

func (f *FromValidatorVerifier) initValidators(validators []*types.Validator) {
	f.setValidators(&types.ValidatorSet{Validators: validators})
}

// setValidators replaces the tracked set with a copy of set, so a later update
// cannot mutate the caller's validators.
func (f *FromValidatorVerifier) setValidators(set *types.ValidatorSet) {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	f.validators = set.Copy()
	f.indexBlsKeys()
}

func (f *FromValidatorVerifier) updateValidators(changes []*types.Validator) error {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	// UpdateWithChangeSet leaves the set untouched when it rejects a batch.
	err := f.validators.UpdateWithChangeSet(changes)
	f.indexBlsKeys()
	return err
}

// indexBlsKeys rebuilds the BLS key index from the tracked set. Callers hold mtx.
func (f *FromValidatorVerifier) indexBlsKeys() {
	f.blsKeys = make(map[string]*types.Validator, len(f.validators.Validators))
	for _, val := range f.validators.Validators {
		if len(val.BlsKey) > 0 {
			f.blsKeys[string(val.BlsKey[:])] = val
		}
	}
}

// lenOfValidators reports how many validators can currently sign votes, i.e. how
// many of them carry a BLS key.
func (f *FromValidatorVerifier) lenOfValidators() int {
	f.mtx.RLock()
	defer f.mtx.RUnlock()

	return len(f.blsKeys)
}

// Validate implements Verifier.
func (f *FromValidatorVerifier) Validate(vote *Vote) error {
	f.mtx.RLock()
	defer f.mtx.RUnlock()

	if _, ok := f.blsKeys[string(vote.PubKey[:])]; ok {
		return nil
	}
	return errors.New("vote is not from validators")
}

// BlsSignatureVerifier will check whether the Vote is correctly bls signed.
type BlsSignatureVerifier struct {
}

// Validate implements Verifier.
func (b *BlsSignatureVerifier) Validate(vote *Vote) error {
	valid := verifySignature(vote.SignBytes(), vote.PubKey, vote.Signature)
	if !valid {
		return errors.New("invalid signature")
	}
	return nil
}

func verifySignature(msg []byte, pubKey, sig []byte) bool {
	blsPubKey, err := bls.UnmarshalPublicKey(pubKey)
	if err != nil {
		return false
	}
	signature, err := bls.UnmarshalSignature(sig)
	if err != nil {
		return false
	}
	return signature.Verify(blsPubKey, msg, DST)
}
