// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package peer

import (
	"testing"
	"time"

	"github.com/btcsuite/btcd/wire/v2"
)

// newPingRTTTestPeer returns an unconnected inbound peer that negotiates the
// current protocol version and is therefore eligible for ping/pong
// statistics.  It is intended for whitebox tests of the ping statistics.
func newPingRTTTestPeer() *Peer {
	p := NewInboundPeer(&Config{})
	p.flagsMtx.Lock()
	p.protocolVersion = MaxProtocolVersion
	p.flagsMtx.Unlock()
	return p
}

// setPendingPing marks the peer as having a pending ping with the passed
// nonce, emulating outHandler immediately before a ping is written.
func (p *Peer) setPendingPing(nonce uint64) {
	p.statsMtx.Lock()
	p.lastPingNonce = nonce
	p.lastPingTime = time.Now()
	p.statsMtx.Unlock()
}

// TestPingRTTStatsNeverCompleted ensures a peer that never received a matching
// pong is explicitly reported as having no completed ping even though the
// zero-value RTT field reads as zero microseconds.
func TestPingRTTStatsNeverCompleted(t *testing.T) {
	t.Parallel()

	p := newPingRTTTestPeer()

	if p.LastPingCompleted() {
		t.Fatal("fresh peer must not report a completed ping")
	}

	snap := p.PingRTTStats()
	if snap.PingCompleted {
		t.Fatal("fresh peer snapshot must report PingCompleted false")
	}
	if snap.PingPending {
		t.Fatal("fresh peer snapshot must not report a pending ping")
	}
	if snap.LastRTTMicros != 0 {
		t.Fatalf("fresh peer snapshot must report zero RTT, got %d",
			snap.LastRTTMicros)
	}
	if !snap.Inbound {
		t.Fatal("NewInboundPeer must report an inbound connection")
	}

	// The generic statistics snapshot must carry the same distinction.
	if p.StatsSnapshot().LastPingCompleted {
		t.Fatal("StatsSnapshot must not report a completed ping")
	}
}

// TestPingRTTStatsMatchingPong ensures a pong whose nonce matches the pending
// ping nonce records a completed measurement and clears the pending state.
func TestPingRTTStatsMatchingPong(t *testing.T) {
	t.Parallel()

	const nonce = uint64(0xDEADBEEF)
	p := newPingRTTTestPeer()
	p.setPendingPing(nonce)

	if !p.PingRTTStats().PingPending {
		t.Fatal("expected a pending ping before the pong")
	}

	p.handlePongMsg(wire.NewMsgPong(nonce))

	if nonce := p.LastPingNonce(); nonce != 0 {
		t.Fatalf("pending nonce must clear, got %d", nonce)
	}
	if !p.LastPingCompleted() {
		t.Fatal("matching pong must mark the ping as completed")
	}

	snap := p.PingRTTStats()
	if !snap.PingCompleted {
		t.Fatal("snapshot must report a completed ping")
	}
	if snap.PingPending {
		t.Fatal("snapshot must not report the ping as pending")
	}
	if snap.LastRTTMicros < 0 {
		t.Fatalf("RTT must not be negative, got %d", snap.LastRTTMicros)
	}
}

// TestPingRTTStatsMismatchedPong ensures a pong that does not match a pending
// ping is ignored, including when a different ping is still outstanding.
func TestPingRTTStatsMismatchedPong(t *testing.T) {
	t.Parallel()

	const wantNonce = uint64(42)
	p := newPingRTTTestPeer()
	p.setPendingPing(wantNonce)

	p.handlePongMsg(wire.NewMsgPong(wantNonce + 1))

	if p.LastPingCompleted() {
		t.Fatal("non-matching pong must not complete the ping")
	}
	if nonce := p.LastPingNonce(); nonce != wantNonce {
		t.Fatalf("pending nonce must be retained, got %d", nonce)
	}

	snap := p.PingRTTStats()
	if snap.PingCompleted {
		t.Fatal("non-matching pong must not set PingCompleted")
	}
	if !snap.PingPending {
		t.Fatal("outstanding ping must remain pending")
	}
}

// TestPingRTTStatsUnsolicitedPong ensures a pong with no pending ping is
// ignored rather than recorded as a completed ping.
func TestPingRTTStatsUnsolicitedPong(t *testing.T) {
	t.Parallel()

	p := newPingRTTTestPeer()
	p.handlePongMsg(wire.NewMsgPong(99))

	if p.LastPingCompleted() {
		t.Fatal("unsolicited pong must not complete a ping")
	}
	if p.PingRTTStats().PingPending {
		t.Fatal("unsolicited pong must not mark a ping as pending")
	}
}

// TestPingRTTStatsOldProtocol ensures pings are not recorded for peers that
// negotiate a protocol version older than BIP0031.
func TestPingRTTStatsOldProtocol(t *testing.T) {
	t.Parallel()

	p := NewInboundPeer(&Config{})
	p.flagsMtx.Lock()
	p.protocolVersion = wire.BIP0031Version
	p.flagsMtx.Unlock()

	p.setPendingPing(7)
	p.handlePongMsg(wire.NewMsgPong(7))

	if p.LastPingCompleted() {
		t.Fatal("pre-BIP0031 peers must never record a completed ping")
	}
}

// TestPingRTTStatsZeroMicrosDistinct explicitly distinguishes a peer with no
// completed ping from a peer whose only completed round trip measured zero
// microseconds: both expose zero in the RTT field but only the latter reports
// PingCompleted.
func TestPingRTTStatsZeroMicrosDistinct(t *testing.T) {
	t.Parallel()

	never := newPingRTTTestPeer().PingRTTStats()

	completedZero := newPingRTTTestPeer()
	completedZero.statsMtx.Lock()
	completedZero.lastPingCompleted = true
	completedZero.lastPingMicros = 0
	completedZero.statsMtx.Unlock()
	zeroSnap := completedZero.PingRTTStats()

	if never.LastRTTMicros != 0 || zeroSnap.LastRTTMicros != 0 {
		t.Fatal("test setup requires both RTT values to be zero")
	}
	if never.PingCompleted {
		t.Fatal("peer without a pong must not be considered completed")
	}
	if !zeroSnap.PingCompleted {
		t.Fatal("peer with a zero-microsecond matching pong must be " +
			"considered completed")
	}
}

// TestPingRTTStatsNewPingRetainsCompletion ensures that when a newer ping is
// in flight, the previously completed measurement and completion flag are
// retained while the peer is also reported as pending.
func TestPingRTTStatsNewPingRetainsCompletion(t *testing.T) {
	t.Parallel()

	const firstNonce, secondNonce = uint64(1), uint64(2)
	p := newPingRTTTestPeer()
	p.setPendingPing(firstNonce)
	p.handlePongMsg(wire.NewMsgPong(firstNonce))
	completedMicros := p.LastPingMicros()

	// Emulate outHandler sending a second ping before its pong arrives.
	p.setPendingPing(secondNonce)

	snap := p.PingRTTStats()
	if !snap.PingCompleted {
		t.Fatal("previous completion must be retained")
	}
	if !snap.PingPending {
		t.Fatal("new ping must be reported as pending")
	}
	if snap.LastRTTMicros != completedMicros {
		t.Fatalf("last completed RTT must be retained: got %d, want %d",
			snap.LastRTTMicros, completedMicros)
	}

	// The non-matching first pong is ignored while the second is pending.
	p.handlePongMsg(wire.NewMsgPong(firstNonce))
	if p.LastPingNonce() != secondNonce {
		t.Fatal("stale pong must not clear the current pending nonce")
	}
	if !p.PingRTTStats().PingPending {
		t.Fatal("current ping must remain pending after stale pong")
	}
}
