// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/mempool"
	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
)

// mockPingRTTPeer implements rpcserverPeer for the ping RTT distribution
// tests.
type mockPingRTTPeer struct {
	peer *peer.Peer
}

func (m *mockPingRTTPeer) ToPeer() *peer.Peer      { return m.peer }
func (m *mockPingRTTPeer) IsTxRelayDisabled() bool { return false }
func (m *mockPingRTTPeer) BanScore() uint32        { return 0 }
func (m *mockPingRTTPeer) FeeFilter() int64        { return 0 }

// mockPingRTTConnMgr implements rpcserverConnManager for the ping RTT
// distribution tests.  Every method other than ConnectedPeers fails the test
// because the RPC must be strictly read-only.
type mockPingRTTConnMgr struct {
	t     *testing.T
	peers []rpcserverPeer
}

func (m *mockPingRTTConnMgr) failUnexpected(method string) {
	m.t.Errorf("getpingrttdistribution must not call %s", method)
}

func (m *mockPingRTTConnMgr) Connect(addr string, permanent bool) error {
	m.failUnexpected("Connect")
	return nil
}

func (m *mockPingRTTConnMgr) RemoveByID(id int32) error {
	m.failUnexpected("RemoveByID")
	return nil
}

func (m *mockPingRTTConnMgr) RemoveByAddr(addr string) error {
	m.failUnexpected("RemoveByAddr")
	return nil
}

func (m *mockPingRTTConnMgr) DisconnectByID(id int32) error {
	m.failUnexpected("DisconnectByID")
	return nil
}

func (m *mockPingRTTConnMgr) DisconnectByAddr(addr string) error {
	m.failUnexpected("DisconnectByAddr")
	return nil
}

func (m *mockPingRTTConnMgr) ConnectedCount() int32 { return 0 }

func (m *mockPingRTTConnMgr) NetTotals() (uint64, uint64) { return 0, 0 }

func (m *mockPingRTTConnMgr) ConnectedPeers() []rpcserverPeer {
	return m.peers
}

func (m *mockPingRTTConnMgr) PersistentPeers() []rpcserverPeer {
	m.failUnexpected("PersistentPeers")
	return nil
}

func (m *mockPingRTTConnMgr) BroadcastMessage(msg wire.Message) {
	m.failUnexpected("BroadcastMessage")
}

func (m *mockPingRTTConnMgr) AddRebroadcastInventory(iv *wire.InvVect,
	data interface{}) {

	m.failUnexpected("AddRebroadcastInventory")
}

func (m *mockPingRTTConnMgr) RelayTransactions(txns []*mempool.TxDesc) {}

func (m *mockPingRTTConnMgr) NodeAddresses() []*wire.NetAddressV2 {
	m.failUnexpected("NodeAddresses")
	return nil
}

// uint64Ptr returns a pointer to the passed uint64.
func uint64Ptr(v uint64) *uint64 {
	return &v
}

// TestValidatePingRTTBounds ensures only strictly increasing boundaries pass
// validation.
func TestValidatePingRTTBounds(t *testing.T) {
	t.Parallel()

	valid := [][]uint64{
		nil,
		{},
		{0},
		{100},
		{0, 1, 2},
		{10, 100, 1000, 10000},
	}
	for _, bounds := range valid {
		require.NoError(t, validatePingRTTBounds(bounds),
			"bounds %v should be valid", bounds)
	}

	invalid := [][]uint64{
		{1, 1},       // duplicate
		{2, 1},       // decreasing
		{1, 2, 2, 3}, // duplicate later in the list
		{3, 2, 1},    // decreasing later in the list
	}
	for _, bounds := range invalid {
		err := validatePingRTTBounds(bounds)
		require.Error(t, err, "bounds %v should be invalid", bounds)
		rpcErr, ok := err.(*btcjson.RPCError)
		require.True(t, ok)
		require.Equal(t, btcjson.ErrRPCInvalidParams.Code, rpcErr.Code)
	}
}

// TestNewPingRTTBuckets verifies the bucket layout, including the null
// endpoints of the overflow buckets and the empty-boundaries result.
func TestNewPingRTTBuckets(t *testing.T) {
	t.Parallel()

	// No boundaries yield a non-nil empty slice so it marshals as [].
	buckets := newPingRTTBuckets(nil)
	require.NotNil(t, buckets)
	require.Len(t, buckets, 0)

	buckets = newPingRTTBuckets([]uint64{100, 1000, 10000})
	require.Len(t, buckets, 4)
	require.Nil(t, buckets[0].LowerInclusive)
	require.Equal(t, uint64Ptr(100), buckets[0].UpperExclusive)
	require.Equal(t, uint64Ptr(100), buckets[1].LowerInclusive)
	require.Equal(t, uint64Ptr(1000), buckets[1].UpperExclusive)
	require.Equal(t, uint64Ptr(1000), buckets[2].LowerInclusive)
	require.Equal(t, uint64Ptr(10000), buckets[2].UpperExclusive)
	require.Equal(t, uint64Ptr(10000), buckets[3].LowerInclusive)
	require.Nil(t, buckets[3].UpperExclusive)
}

// TestPingRTTDistribution classifies a deterministic set of peer snapshots
// and covers zero latency, missing completions, pings awaiting a new response,
// bucket boundaries, directionality, and the overflow buckets.
func TestPingRTTDistribution(t *testing.T) {
	t.Parallel()

	sampleTime := time.Unix(1700000000, 0)
	bounds := []uint64{10, 100, 1000}

	completed := func(inbound bool, rtt int64, pending bool) peer.PingRTTSnapshot {
		return peer.PingRTTSnapshot{
			Inbound:       inbound,
			PingCompleted: true,
			PingPending:   pending,
			LastRTTMicros: rtt,
		}
	}
	neverCompleted := func(inbound, pending bool) peer.PingRTTSnapshot {
		return peer.PingRTTSnapshot{
			Inbound:       inbound,
			PingCompleted: false,
			PingPending:   pending,
		}
	}

	peers := []peer.PingRTTSnapshot{
		completed(true, 0, false),     // 0: underflow inbound, zero RTT
		completed(true, 9, false),     // 1: underflow inbound
		completed(false, 10, false),   // 2: interior [10,100) outbound
		completed(false, 99, false),   // 3: interior [10,100) outbound
		completed(true, 100, false),   // 4: interior [100,1000) inbound
		completed(false, 999, false),  // 5: interior [100,1000) outbound
		completed(false, 1000, false), // 6: overflow outbound
		completed(true, 5000, false),  // 7: overflow inbound
		neverCompleted(true, false),   // 8: never completed, not pending
		neverCompleted(false, true),   // 9: never completed + pending
		completed(true, 50, true),     // 10: completed value kept while pending
		completed(true, 0, true),      // 11: zero RTT + pending new ping
	}

	result := pingRTTDistribution(bounds, sampleTime, peers)

	require.Equal(t, sampleTime.Unix(), result.SampleTime)
	require.EqualValues(t, len(peers), result.TotalPeers)
	require.EqualValues(t, 2, result.NeverCompletedPing)
	require.EqualValues(t, 3, result.PingPending)
	require.Len(t, result.Buckets, 4)

	// Buckets must use half-open intervals with proper overflow edges.
	require.Nil(t, result.Buckets[0].LowerInclusive)
	require.Equal(t, uint64Ptr(10), result.Buckets[0].UpperExclusive)
	require.Equal(t, uint64Ptr(10), result.Buckets[1].LowerInclusive)
	require.Equal(t, uint64Ptr(100), result.Buckets[1].UpperExclusive)
	require.Equal(t, uint64Ptr(100), result.Buckets[2].LowerInclusive)
	require.Equal(t, uint64Ptr(1000), result.Buckets[2].UpperExclusive)
	require.Equal(t, uint64Ptr(1000), result.Buckets[3].LowerInclusive)
	require.Nil(t, result.Buckets[3].UpperExclusive)

	// underflow (<10): inbound peers 0, 1, 11
	require.EqualValues(t, 3, result.Buckets[0].Inbound)
	require.EqualValues(t, 0, result.Buckets[0].Outbound)

	// [10,100): inbound peer 10, outbound peers 2, 3
	require.EqualValues(t, 1, result.Buckets[1].Inbound)
	require.EqualValues(t, 2, result.Buckets[1].Outbound)

	// [100,1000): inbound peer 4, outbound peer 5
	require.EqualValues(t, 1, result.Buckets[2].Inbound)
	require.EqualValues(t, 1, result.Buckets[2].Outbound)

	// >=1000: inbound peer 7, outbound peer 6
	require.EqualValues(t, 1, result.Buckets[3].Inbound)
	require.EqualValues(t, 1, result.Buckets[3].Outbound)

	// Every completed peer must be represented exactly once.
	var bucketed uint32
	for i := range result.Buckets {
		bucketed += result.Buckets[i].Inbound
		bucketed += result.Buckets[i].Outbound
	}
	require.EqualValues(t, uint32(len(peers))-result.NeverCompletedPing,
		bucketed)
}

// TestPingRTTDistributionZeroLatencyDistinct explicitly verifies that a peer
// without a completed ping and a peer with a zero microsecond completion are
// handled differently even though both expose a zero RTT value.
func TestPingRTTDistributionZeroLatencyDistinct(t *testing.T) {
	t.Parallel()

	bounds := []uint64{0, 100}
	peers := []peer.PingRTTSnapshot{
		{
			// No completion: must land in the never-completed
			// counter and not in any bucket.
			Inbound:       true,
			PingCompleted: false,
			LastRTTMicros: 0,
		},
		{
			// Zero microsecond completion: must be counted in the
			// [0,100) bucket, never as "below zero".
			Inbound:       false,
			PingCompleted: true,
			LastRTTMicros: 0,
		},
	}

	result := pingRTTDistribution(bounds, time.Now(), peers)
	require.EqualValues(t, 1, result.NeverCompletedPing)
	require.EqualValues(t, 0, result.Buckets[0].Inbound)
	require.EqualValues(t, 0, result.Buckets[0].Outbound)
	require.EqualValues(t, 0, result.Buckets[1].Inbound)
	require.EqualValues(t, 1, result.Buckets[1].Outbound)
}

// TestPingRTTDistributionEmptyInputs ensures both empty peer lists and empty
// boundaries produce deterministic, well-formed results.
func TestPingRTTDistributionEmptyInputs(t *testing.T) {
	t.Parallel()

	// Empty peer list with boundaries.
	result := pingRTTDistribution([]uint64{10, 100}, time.Unix(1, 0), nil)
	require.EqualValues(t, 0, result.TotalPeers)
	require.EqualValues(t, 0, result.NeverCompletedPing)
	require.EqualValues(t, 0, result.PingPending)
	require.Len(t, result.Buckets, 3)
	for i := range result.Buckets {
		require.EqualValues(t, 0, result.Buckets[i].Inbound)
		require.EqualValues(t, 0, result.Buckets[i].Outbound)
	}

	// Empty boundaries with peers produce no buckets but accurate counts.
	peers := []peer.PingRTTSnapshot{
		{PingCompleted: false, PingPending: true, Inbound: true},
		{PingCompleted: true, LastRTTMicros: 42, Inbound: false},
	}
	result = pingRTTDistribution(nil, time.Unix(1, 0), peers)
	require.EqualValues(t, 2, result.TotalPeers)
	require.EqualValues(t, 1, result.NeverCompletedPing)
	require.EqualValues(t, 1, result.PingPending)
	require.NotNil(t, result.Buckets)
	require.Len(t, result.Buckets, 0)

	// The result must marshal buckets as an empty JSON array and render
	// null bucket endpoints elsewhere.
	bucketsJSON, err := json.Marshal(result.Buckets)
	require.NoError(t, err)
	require.Equal(t, `[]`, string(bucketsJSON))

	withBounds := pingRTTDistribution([]uint64{5}, time.Unix(1, 0),
		nil)
	raw, err := json.Marshal(withBounds.Buckets)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"lower_inclusive":null`)
	require.Contains(t, string(raw), `"upper_exclusive":null`)
}

// TestHandleGetPingRTTDistributionInvalidBounds ensures non-monotonic
// boundaries fail with an invalid parameters error before peers are read.
func TestHandleGetPingRTTDistributionInvalidBounds(t *testing.T) {
	t.Parallel()

	s := &rpcServer{}
	for _, bounds := range [][]uint64{{3, 3}, {9, 2}} {
		cmd := &btcjson.GetPingRTTDistributionCmd{Bounds: bounds}
		result, err := handleGetPingRTTDistribution(s, cmd, nil)
		require.Error(t, err)
		require.Nil(t, result)
		rpcErr, ok := err.(*btcjson.RPCError)
		require.True(t, ok)
		require.Equal(t, btcjson.ErrRPCInvalidParams.Code, rpcErr.Code)
	}
}

// TestHandleGetPingRTTDistribution walks the handler end to end using a mock
// connection manager backed by real, unconnected peers, which have no
// completed ping by definition.
func TestHandleGetPingRTTDistribution(t *testing.T) {
	t.Parallel()

	const numInbound = 3
	rpcPeers := make([]rpcserverPeer, numInbound)
	realPeers := make([]*peer.Peer, numInbound)
	for i := range rpcPeers {
		realPeers[i] = peer.NewInboundPeer(&peer.Config{})
		rpcPeers[i] = &mockPingRTTPeer{peer: realPeers[i]}
	}

	connMgr := &mockPingRTTConnMgr{t: t, peers: rpcPeers}
	s := &rpcServer{cfg: rpcserverConfig{ConnMgr: connMgr}}

	cmd := &btcjson.GetPingRTTDistributionCmd{
		Bounds: []uint64{100, 1000},
	}
	result, err := handleGetPingRTTDistribution(s, cmd, nil)
	require.NoError(t, err)

	rttResult, ok := result.(*btcjson.GetPingRTTDistributionResult)
	require.True(t, ok)
	require.EqualValues(t, numInbound, rttResult.TotalPeers)
	require.EqualValues(t, numInbound, rttResult.NeverCompletedPing)
	require.EqualValues(t, 0, rttResult.PingPending)
	require.Len(t, rttResult.Buckets, 3)
	var total uint32
	for i := range rttResult.Buckets {
		total += rttResult.Buckets[i].Inbound +
			rttResult.Buckets[i].Outbound
	}
	require.EqualValues(t, 0, total)
	require.NotZero(t, rttResult.SampleTime)
}

// TestHandleGetPingRTTDistributionDisconnectRace invokes the handler
// concurrently while the sampled peers disconnect, ensuring per-peer snapshot
// reads remain safe during teardown.
func TestHandleGetPingRTTDistributionDisconnectRace(t *testing.T) {
	t.Parallel()

	const numPeers = 8
	rpcPeers := make([]rpcserverPeer, numPeers)
	realPeers := make([]*peer.Peer, numPeers)
	for i := range rpcPeers {
		realPeers[i] = peer.NewInboundPeer(&peer.Config{})
		rpcPeers[i] = &mockPingRTTPeer{peer: realPeers[i]}
	}

	connMgr := &mockPingRTTConnMgr{t: t, peers: rpcPeers}
	s := &rpcServer{cfg: rpcserverConfig{ConnMgr: connMgr}}
	cmd := &btcjson.GetPingRTTDistributionCmd{
		Bounds: []uint64{10, 100, 1000},
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				res, err := handleGetPingRTTDistribution(s, cmd, nil)
				require.NoError(t, err)
				require.EqualValues(t, numPeers,
					res.(*btcjson.GetPingRTTDistributionResult).
						TotalPeers)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, p := range realPeers {
			p.Disconnect()
		}
	}()

	wg.Wait()
	for _, p := range realPeers {
		p.WaitForDisconnect()
	}

	// A final query after all peers disconnected must still succeed.
	res, err := handleGetPingRTTDistribution(s, cmd, nil)
	require.NoError(t, err)
	rttResult := res.(*btcjson.GetPingRTTDistributionResult)
	require.EqualValues(t, numPeers, rttResult.TotalPeers)
	require.EqualValues(t, numPeers, rttResult.NeverCompletedPing)
}
