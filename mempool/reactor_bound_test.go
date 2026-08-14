package mempool

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMempoolPacketChannelBounded guards the recv/checkTx queue depth: memory is
// depth x max envelope, so it must stay bounded. Fails against the old 204800.
func TestMempoolPacketChannelBounded(t *testing.T) {
	require.LessOrEqual(t, MempoolPacketChannelSize, 8192,
		"mempool recv/checkTx queue depth must stay bounded to cap resident memory")
}
