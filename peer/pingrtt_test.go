// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package peer_test

import (
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire/v2"
)

// waitForCondition polls cond until it returns true or the timeout elapses.
func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool,
	desc string) {

	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

// scriptedRemotePeer is an outbound peer connected to a test-driven remote
// endpoint over in-memory pipes.  The test performs the version handshake and
// reads each outbound message, allowing it to choose when (or whether) to
// reply with a pong.
type scriptedRemotePeer struct {
	t        *testing.T
	peer     *peer.Peer
	remote   *conn
	outbound <-chan wire.Message
}

// newScriptedRemotePeer creates an outbound peer attached to a scripted
// remote endpoint that has completed the protocol handshake.
func newScriptedRemotePeer(t *testing.T) *scriptedRemotePeer {
	t.Helper()

	verack := make(chan struct{}, 1)
	cfg := &peer.Config{
		Listeners: peer.MessageListeners{
			OnVerAck: func(p *peer.Peer, msg *wire.MsgVerAck) {
				verack <- struct{}{}
			},
		},
		ChainParams:     &chaincfg.MainNetParams,
		TrickleInterval: time.Second * 10,
		AllowSelfConns:  true,
	}

	localConn, remoteConn := pipe(
		&conn{laddr: "10.0.0.1:8333", raddr: "10.0.0.2:8333"},
		&conn{laddr: "10.0.0.2:8333", raddr: "10.0.0.1:8333"},
	)

	p, err := peer.NewOutboundPeer(cfg, "10.0.0.2:8333")
	if err != nil {
		t.Fatalf("NewOutboundPeer: unexpected error: %v", err)
	}
	p.AssociateConnection(localConn)

	outbound := make(chan wire.Message, 16)
	go func() {
		for {
			_, msg, _, err := wire.ReadMessageN(
				remoteConn, p.ProtocolVersion(),
				cfg.ChainParams.Net,
			)
			if err != nil {
				close(outbound)
				return
			}
			outbound <- msg
		}
	}()

	s := &scriptedRemotePeer{
		t:        t,
		peer:     p,
		remote:   remoteConn,
		outbound: outbound,
	}

	// Complete the handshake: read the peer's version, reply with a
	// matching version and verack, then consume the peer's verack.
	select {
	case msg := <-outbound:
		versionMsg, ok := msg.(*wire.MsgVersion)
		if !ok {
			t.Fatalf("expected version message, got %s",
				msg.Command())
		}

		reply := wire.NewMsgVersion(&versionMsg.AddrYou,
			&versionMsg.AddrMe, versionMsg.Nonce, 0)
		if err := s.write(reply); err != nil {
			t.Fatalf("write version: unexpected error: %v", err)
		}
		if err := s.write(wire.NewMsgVerAck()); err != nil {
			t.Fatalf("write verack: unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for version message")
	}

	// The peer emits sendaddrv2 before verack during negotiation, so
	// consume messages until the verack arrives.
	for {
		select {
		case msg := <-outbound:
			if _, ok := msg.(*wire.MsgVerAck); ok {
				goto negotiated
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for outbound verack")
		}
	}
negotiated:

	select {
	case <-verack:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for negotiated verack")
	}

	return s
}

// write sends a message to the peer from the scripted remote endpoint.
func (s *scriptedRemotePeer) write(msg wire.Message) error {
	_, err := wire.WriteMessageN(s.remote, msg,
		s.peer.ProtocolVersion(), chaincfg.MainNetParams.Net)
	return err
}

// readOutbound waits for the next message sent by the peer.
func (s *scriptedRemotePeer) readOutbound() wire.Message {
	select {
	case msg, ok := <-s.outbound:
		if !ok {
			s.t.Fatal("scripted remote connection closed")
		}
		return msg
	case <-time.After(2 * time.Second):
		s.t.Fatal("timeout waiting for outbound message")
	}
	return nil
}

// close disconnects the peer and closes the scripted connection.
func (s *scriptedRemotePeer) close() {
	s.peer.Disconnect()
	s.peer.WaitForDisconnect()
	_ = s.remote.Close()
}

// TestPingRTTStatsEndToEnd drives real ping and pong messages with a
// scripted remote and verifies completion, pending, and inbound/outbound
// snapshot states, including the retained completion while a new response is
// outstanding and the explicit zero-RTT versus no-completion distinction.
func TestPingRTTStatsEndToEnd(t *testing.T) {
	s := newScriptedRemotePeer(t)
	defer s.close()

	// The outbound peer has no completed ping initially.
	snap := s.peer.PingRTTStats()
	if snap.PingCompleted || snap.PingPending || snap.Inbound {
		s.t.Fatalf("unexpected initial snapshot: %+v", snap)
	}

	// An unsolicited pong with no pending ping is ignored.
	if err := s.write(wire.NewMsgPong(9999)); err != nil {
		t.Fatalf("write unsolicited pong: unexpected error: %v", err)
	}
	if s.peer.LastPingCompleted() {
		t.Fatal("unsolicited pong must not complete a ping")
	}

	// First ping: the scripted remote replies with a matching pong.
	const firstNonce = uint64(0x5A5A)
	s.peer.QueueMessage(wire.NewMsgPing(firstNonce), nil)
	msg := s.readOutbound()
	if ping, ok := msg.(*wire.MsgPing); !ok || ping.Nonce != firstNonce {
		t.Fatalf("expected ping %d, got %v", firstNonce, msg)
	}
	if err := s.write(wire.NewMsgPong(firstNonce)); err != nil {
		t.Fatalf("write first pong: unexpected error: %v", err)
	}

	waitForCondition(t, 2*time.Second, s.peer.LastPingCompleted,
		"matching pong to complete the first ping")
	if nonce := s.peer.LastPingNonce(); nonce != 0 {
		t.Fatalf("pending nonce must clear after pong, got %d", nonce)
	}
	firstMicros := s.peer.LastPingMicros()
	snap = s.peer.PingRTTStats()
	if !snap.PingCompleted || snap.PingPending || snap.Inbound {
		t.Fatalf("unexpected snapshot after first pong: %+v", snap)
	}

	// Second ping: hold back the pong so the pending state is observed
	// deterministically.  The previous completion must be retained.
	const secondNonce = uint64(0xA5A5)
	s.peer.QueueMessage(wire.NewMsgPing(secondNonce), nil)
	msg = s.readOutbound()
	if ping, ok := msg.(*wire.MsgPing); !ok || ping.Nonce != secondNonce {
		t.Fatalf("expected ping %d, got %v", secondNonce, msg)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return s.peer.LastPingNonce() == secondNonce
	}, "second ping to be marked pending")
	snap = s.peer.PingRTTStats()
	if !snap.PingCompleted {
		t.Fatal("previous completion must be retained while a new " +
			"ping is pending")
	}
	if !snap.PingPending {
		t.Fatal("second ping must be reported as pending")
	}
	if snap.LastRTTMicros != firstMicros {
		t.Fatalf("retained RTT mismatch: got %d, want %d",
			snap.LastRTTMicros, firstMicros)
	}

	// A stale pong for the first ping must not satisfy the second one.
	if err := s.write(wire.NewMsgPong(firstNonce)); err != nil {
		t.Fatalf("write stale pong: unexpected error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if nonce := s.peer.LastPingNonce(); nonce != secondNonce {
		t.Fatalf("stale pong must not clear pending nonce, got %d",
			nonce)
	}

	// Matching pong for the second ping completes it.
	if err := s.write(wire.NewMsgPong(secondNonce)); err != nil {
		t.Fatalf("write second pong: unexpected error: %v", err)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		return s.peer.LastPingNonce() == 0
	}, "matching pong to complete the second ping")
	snap = s.peer.PingRTTStats()
	if !snap.PingCompleted || snap.PingPending {
		t.Fatalf("unexpected snapshot after second pong: %+v", snap)
	}
}

// TestPingRTTStatsDirectionReports verifies the direction flag of snapshots
// for a handshaked inbound/outbound TCP peer pair.
func TestPingRTTStatsDirectionReports(t *testing.T) {
	inPeer, outPeer := newHandshakedPingPeers(t)
	defer func() {
		inPeer.Disconnect()
		outPeer.Disconnect()
		inPeer.WaitForDisconnect()
		outPeer.WaitForDisconnect()
	}()

	if !inPeer.PingRTTStats().Inbound {
		t.Fatal("inbound peer must report Inbound true")
	}
	if outPeer.PingRTTStats().Inbound {
		t.Fatal("outbound peer must report Inbound false")
	}
}

// newHandshakedPingPeers returns an inbound and an outbound peer that have
// completed the protocol handshake over a local TCP connection.
func newHandshakedPingPeers(t *testing.T) (*peer.Peer, *peer.Peer) {
	t.Helper()

	verack := make(chan struct{}, 4)
	cfg := &peer.Config{
		Listeners: peer.MessageListeners{
			OnVerAck: func(p *peer.Peer, msg *wire.MsgVerAck) {
				verack <- struct{}{}
			},
			OnWrite: func(p *peer.Peer, bytesWritten int,
				msg wire.Message, err error) {

				if _, ok := msg.(*wire.MsgVerAck); ok {
					verack <- struct{}{}
				}
			},
		},
		TrickleInterval: time.Second * 10,
		AllowSelfConns:  true,
	}

	inPeer := peer.NewInboundPeer(cfg)
	outPeer, err := peer.NewOutboundPeer(cfg, "10.0.0.2:8333")
	if err != nil {
		t.Fatalf("NewOutboundPeer: unexpected error: %v", err)
	}

	if err := setupPeerConnection(inPeer, outPeer); err != nil {
		t.Fatalf("setupPeerConnection: unexpected error: %v", err)
	}

	for i := 0; i < 4; i++ {
		select {
		case <-verack:
		case <-time.After(2 * time.Second):
			t.Fatal("verack handshake timeout")
		}
	}

	return inPeer, outPeer
}

// TestPingRTTStatsDisconnectRace exercises statistics snapshot access
// concurrently with ping traffic and disconnection to ensure the data is read
// safely while peers are tearing down.
func TestPingRTTStatsDisconnectRace(t *testing.T) {
	const iterations = 25

	for i := 0; i < iterations; i++ {
		inPeer, outPeer := newHandshakedPingPeers(t)

		var wg sync.WaitGroup
		wg.Add(4)

		snapshotPeer := func(p *peer.Peer) {
			defer wg.Done()
			for {
				select {
				case <-p.Done():
					// A final snapshot after disconnect
					// must remain safe.
					_ = p.PingRTTStats()
					_ = p.StatsSnapshot()
					return
				default:
				}

				_ = p.PingRTTStats()
				_ = p.StatsSnapshot()
				_ = p.LastPingNonce()
				_ = p.LastPingCompleted()
			}
		}

		go snapshotPeer(inPeer)
		go snapshotPeer(outPeer)

		go func() {
			defer wg.Done()
			// Keep the number of queued messages below the output
			// queue buffer size.  The inbound peer automatically
			// replies with matching pongs.
			for n := uint64(1); n <= 20; n++ {
				select {
				case <-outPeer.Done():
					return
				default:
				}
				outPeer.QueueMessage(wire.NewMsgPing(n), nil)
			}
		}()

		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
			inPeer.Disconnect()
			outPeer.Disconnect()
		}()

		wg.Wait()
		inPeer.WaitForDisconnect()
		outPeer.WaitForDisconnect()
	}
}
