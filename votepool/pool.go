package votepool

import (
	"context"
	"errors"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru"

	"github.com/cometbft/cometbft/libs/service"

	"github.com/cometbft/cometbft/libs/log"

	cmtpubsub "github.com/cometbft/cometbft/libs/pubsub"
	"github.com/cometbft/cometbft/libs/sync"
	"github.com/cometbft/cometbft/types"
)

const (

	// The number of cached votes (i.e., keys) to quickly filter out when adding votes.
	cacheVoteSize = 1024

	// The number of (vote, signature) pairs remembered after they failed BLS
	// verification, so an identical resubmission is dropped without repeating
	// the BN254 pairing.
	negativeCacheVoteSize = 4096

	// Vote will be assigned the expired at time when adding to the Pool.
	voteKeepAliveAfter = time.Second * 30

	// Votes in the Pool will be pruned periodically to remove useless ones.
	pruneVoteInterval = 3 * time.Second

	// Defines the channel size for event bus subscription.
	eventBusSubscribeCap = 1024

	// The event type of adding new votes to the Pool successfully.
	eventBusVotePoolUpdates = "votePoolUpdates"

	// Event bus client ID used for the validator-update subscription.
	votePoolSubscriber = "VotePoolService"

	// Delay before re-subscribing after the validator-update subscription is
	// canceled. Without it a persistently failing subscription spins the routine
	// with no pause, burning CPU and flooding logs.
	resubscribeBackoff = time.Second
)

// voteStore stores one type of votes.
type voteStore struct {
	mtx     *sync.RWMutex               // mutex for concurrency access of voteMap and others
	voteMap map[string]map[string]*Vote // map: eventHash -> pubKey -> Vote

	queue *VoteQueue // priority queue for prune votes
}

// newVoteStore creates a store to store votes.
func newVoteStore() *voteStore {
	s := &voteStore{
		mtx:     &sync.RWMutex{},
		voteMap: make(map[string]map[string]*Vote),
		queue:   NewVoteQueue(),
	}
	return s
}

// addVote will add a vote to the store.
// Be noted: no validation is conducted in this layer.
func (s *voteStore) addVote(vote *Vote) {
	eventHashStr := string(vote.EventHash[:])
	pubKeyStr := string(vote.PubKey[:])
	s.mtx.Lock()
	defer s.mtx.Unlock()

	subM, ok := s.voteMap[eventHashStr]
	if !ok {
		subM = make(map[string]*Vote)
		s.voteMap[eventHashStr] = subM
	}
	subM[pubKeyStr] = vote
	s.queue.Insert(vote)
}

// getVotesByEventHash will query events by event hash.
func (s *voteStore) getVotesByEventHash(eventHash []byte) []*Vote {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	votes := make([]*Vote, 0)
	if subM, ok := s.voteMap[string(eventHash[:])]; ok {
		for _, v := range subM {
			votes = append(votes, v)
		}
	}
	return votes
}

// getAllVotes will return all votes in the store.
func (s *voteStore) getAllVotes() []*Vote {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	votes := make([]*Vote, 0)
	for _, subM := range s.voteMap {
		for _, v := range subM {
			votes = append(votes, v)
		}
	}
	return votes
}

// flushVotes will clear all votes in the store.
func (s *voteStore) flushVotes() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.voteMap = make(map[string]map[string]*Vote)
	s.queue = NewVoteQueue()
}

// pruneVotes will prune votes which are expired and return the pruned votes' keys.
func (s *voteStore) pruneVotes() []string {
	keys := make([]string, 0)
	current := &Vote{expireAt: time.Now()}
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if expires, err := s.queue.PopUntil(current); err == nil {
		for _, expire := range expires {
			// The same (EventHash, PubKey) can be inserted more than once -- if the
			// dedup cache evicted its key, a re-submission reaches addVote again and
			// overwrites the map entry while leaving the older queue entry in place.
			// Only drop the map entry when it is still the vote that just expired,
			// otherwise the stale queue entry evicts a live vote early.
			// The string() conversions stay inline so the compiler can elide the
			// allocation on map access (staticcheck SA6001).
			if subM, ok := s.voteMap[string(expire.EventHash[:])]; ok {
				if live, ok := subM[string(expire.PubKey[:])]; ok && live != expire {
					continue
				}
				delete(subM, string(expire.PubKey[:]))
			}
			keys = append(keys, expire.Key())
		}
	}
	return keys
}

// Pool implements VotePool to store different types of votes.
// Meanwhile, it will check the signature and source signer of a vote, only votes from validators will be saved.
type Pool struct {
	service.BaseService

	stores map[EventType]*voteStore // each event type will have a store
	ticker *time.Ticker             // prune ticker

	blsVerifier       *BlsSignatureVerifier  // verify a vote's signature
	validatorVerifier *FromValidatorVerifier // verify a vote is from a validator

	cache *lru.Cache // to cache recent added votes' keys

	// negCache remembers (vote key, signature) pairs that already failed BLS
	// verification. Verification is the expensive step, and nothing else stops a
	// peer replaying the same bad signature to force the pairing every time.
	negCache *lru.Cache

	eventBus *types.EventBus // to subscribe validator update events and publish new added vote events
}

// NewVotePool creates a Pool. The initial validators should be supplied.
func NewVotePool(logger log.Logger, validators []*types.Validator, eventBus *types.EventBus) *Pool {
	eventTypes := []EventType{ToBscCrossChainEvent, FromBscCrossChainEvent, DataAvailabilityChallengeEvent, FromOpCrossChainEvent, ToOpCrossChainEvent}

	ticker := time.NewTicker(pruneVoteInterval)
	stores := make(map[EventType]*voteStore, len(eventTypes))
	for _, et := range eventTypes {
		store := newVoteStore()
		stores[et] = store
	}

	cache, _ := lru.New(cacheVoteSize)            // positive parameter will never return error
	negCache, _ := lru.New(negativeCacheVoteSize) // positive parameter will never return error

	// set the initial validators
	validatorVerifier := NewFromValidatorVerifier()
	validatorVerifier.initValidators(validators)
	votePool := &Pool{
		stores:            stores,
		ticker:            ticker,
		cache:             cache,
		negCache:          negCache,
		eventBus:          eventBus,
		blsVerifier:       &BlsSignatureVerifier{},
		validatorVerifier: validatorVerifier,
	}
	votePool.BaseService = *service.NewBaseService(logger, "VotePool", votePool)

	return votePool
}

// OnStart implements Service.
func (p *Pool) OnStart() error {
	if err := p.BaseService.OnStart(); err != nil {
		return err
	}
	go p.validatorUpdateRoutine()
	go p.pruneVoteRoutine()
	return nil
}

// OnStop implements Service.
func (p *Pool) OnStop() {
	p.BaseService.OnStop()
	p.ticker.Stop()
	// Without this the event bus keeps the "VotePoolService" client registered,
	// so a restart re-subscribing with the same ID gets ErrAlreadySubscribed and
	// validatorUpdateRoutine exits immediately -- validator updates silently stop.
	// A canceled subscription is already removed by pubsub, so "not found" here
	// is the normal path after a cancellation rather than a failure.
	if err := p.eventBus.UnsubscribeAll(context.Background(), votePoolSubscriber); err != nil &&
		!errors.Is(err, cmtpubsub.ErrSubscriptionNotFound) {
		p.Logger.Error("Cannot unsubscribe from event bus", "err", err.Error())
	}
}

// AddVote implements VotePool.
func (p *Pool) AddVote(vote *Vote) error {
	err := vote.ValidateBasic()
	if err != nil {
		return err
	}
	store, ok := p.stores[vote.EventType]
	if !ok {
		return errors.New("unsupported event type")
	}

	if ok = p.cache.Contains(vote.Key()); ok {
		return nil
	}

	if err = p.validatorVerifier.Validate(vote); err != nil {
		return err
	}
	// Keyed on the signature too: a later vote carrying a correct signature for
	// the same event and key must still be accepted.
	negKey := vote.Key() + string(vote.Signature)
	if p.negCache.Contains(negKey) {
		return errors.New("vote signature previously failed verification")
	}
	if err = p.blsVerifier.Validate(vote); err != nil {
		p.negCache.Add(negKey, struct{}{})
		return err
	}

	vote.expireAt = time.Now().Add(voteKeepAliveAfter)
	store.addVote(vote)

	if err = p.eventBus.Publish(eventBusVotePoolUpdates, *vote); err != nil {
		p.Logger.Error("Cannot publish vote pool event", "err", err.Error())
	}
	p.cache.Add(vote.Key(), struct{}{})
	return nil
}

// GetVotesByEventTypeAndHash implements VotePool.
func (p *Pool) GetVotesByEventTypeAndHash(eventType EventType, eventHash []byte) ([]*Vote, error) {
	store, ok := p.stores[eventType]
	if !ok {
		return nil, errors.New("unsupported event type")
	}
	return store.getVotesByEventHash(eventHash), nil
}

// GetVotesByEventType implements VotePool.
func (p *Pool) GetVotesByEventType(eventType EventType) ([]*Vote, error) {
	store, ok := p.stores[eventType]
	if !ok {
		return nil, errors.New("unsupported event type")
	}
	return store.getAllVotes(), nil
}

// FlushVotes implements VotePool.
func (p *Pool) FlushVotes() {
	for _, store := range p.stores {
		store.flushVotes()
	}
	p.cache.Purge()
	p.negCache.Purge()
}

// validatorUpdateRoutine will sync validator updates.
//
// A canceled subscription is recoverable: pubsub cancels the whole subscription
// when its buffer overflows, and simply returning here would freeze the validator
// set for the lifetime of the process -- removed validators would keep being
// accepted and new ones rejected. Resubscribe instead.
func (p *Pool) validatorUpdateRoutine() {
	// Always release the event-bus client on the way out. OnStop cannot do this
	// alone: it may run before this routine has subscribed at all, in which case
	// the Subscribe below lands afterwards and the registration would leak for
	// the lifetime of the bus, locking out every later pool with "already
	// subscribed".
	//
	// The client ID is a constant, so on an event bus shared by more than one
	// pool this also drops the other pool's subscription. A node runs a single
	// pool (see node/setup.go), so that only arises in tests and embedded uses,
	// and the other pool recovers through the resubscribe loop below -- at the
	// cost of one backoff interval without validator updates.
	defer func() {
		if err := p.eventBus.UnsubscribeAll(context.Background(), votePoolSubscriber); err != nil &&
			!errors.Is(err, cmtpubsub.ErrSubscriptionNotFound) {
			p.Logger.Error("Cannot unsubscribe on exit", "err", err.Error())
		}
	}()

	for {
		if !p.IsRunning() || !p.eventBus.IsRunning() {
			return
		}
		sub, err := p.eventBus.Subscribe(context.Background(), votePoolSubscriber, types.EventQueryValidatorSetUpdates, eventBusSubscribeCap)
		if err != nil {
			// Recoverable: a predecessor pool on this bus may still be releasing
			// the same client ID, which surfaces as "already subscribed". Giving
			// up here would freeze the validator set exactly as a cancellation
			// would, so retry rather than exit.
			p.Logger.Error("Cannot subscribe to validator set update event; retrying", "err", err.Error())
			if !p.waitBeforeRetry() {
				return
			}
			continue
		}
		if resubscribe := p.consumeValidatorUpdates(sub); !resubscribe {
			return
		}
		p.Logger.Error("Validator update subscription canceled; resubscribing")
		// pubsub removes a subscription before canceling it, so by the time we get
		// here there is usually nothing left to unsubscribe, and NotFound is the
		// expected result. Nothing here is worth exiting for: if the old
		// subscription did somehow survive, that surfaces as ErrAlreadySubscribed
		// on the next Subscribe, which is retried at the top of the loop. Exiting
		// would freeze the validator set -- the exact failure this loop exists to
		// prevent.
		if err := p.eventBus.UnsubscribeAll(context.Background(), votePoolSubscriber); err != nil &&
			!errors.Is(err, cmtpubsub.ErrSubscriptionNotFound) {
			p.Logger.Error("Cannot unsubscribe before resubscribing", "err", err.Error())
		}
		if !p.waitBeforeRetry() {
			return
		}
	}
}

// waitBeforeRetry pauses before another subscribe attempt, staying responsive to
// shutdown. It reports whether the caller should keep going.
func (p *Pool) waitBeforeRetry() bool {
	select {
	case <-time.After(resubscribeBackoff):
		return true
	case <-p.Quit():
		return false
	}
}

// consumeValidatorUpdates drains a subscription, reporting whether the caller
// should resubscribe (true) or shut down (false).
func (p *Pool) consumeValidatorUpdates(sub types.Subscription) bool {
	for {
		select {
		case validatorData := <-sub.Out():
			// Two-return form: a different payload published under this query would
			// otherwise panic the routine.
			changes, ok := validatorData.Data().(types.EventDataValidatorSetUpdates)
			if !ok {
				p.Logger.Error("Unexpected payload on validator set update event",
					"type", fmt.Sprintf("%T", validatorData.Data()))
				continue
			}
			if err := p.validatorVerifier.updateValidators(changes.ValidatorUpdates); err != nil {
				p.Logger.Error("Validator set update applied with errors", "err", err.Error())
			}
			p.Logger.Info("Validators updated", "changes", changes.ValidatorUpdates)
		case <-sub.Canceled():
			return true
		case <-p.Quit():
			return false
		}
	}
}

// pruneVoteRoutine will prune votes at the given intervals.
func (p *Pool) pruneVoteRoutine() {
	for {
		select {
		case <-p.ticker.C:
			for _, s := range p.stores {
				keys := s.pruneVotes()
				for _, key := range keys {
					p.cache.Remove(key)
				}
			}
		// Ranging over the ticker alone leaked this goroutine on every stop.
		case <-p.Quit():
			return
		}
	}
}
