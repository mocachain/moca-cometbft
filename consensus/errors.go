package consensus

// Synced from upstream CometBFT v0.38.x.

// ErrInvalidVote is returned when a vote cannot be processed, e.g. because it
// references a validator index that is not part of the current validator set.
type ErrInvalidVote struct {
	Reason string
}

func (e ErrInvalidVote) Error() string {
	return "invalid vote: " + e.Reason
}
