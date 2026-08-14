package votepool

import (
	"bytes"
	"context"
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
