// Copyright (c) 2016-2017 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcjson

// VersionResult models objects included in the version response.  In the actual
// result, these objects are keyed by the program or API name.
//
// NOTE: This is a btcsuite extension ported from
// github.com/decred/dcrd/dcrjson.
type VersionResult struct {
	VersionString string `json:"versionstring"`
	Major         uint32 `json:"major"`
	Minor         uint32 `json:"minor"`
	Patch         uint32 `json:"patch"`
	Prerelease    string `json:"prerelease"`
	BuildMetadata string `json:"buildmetadata"`
}

// NetworkGroupResult models the distribution of the currently connected peers
// within a single network group.  It is part of the result returned from the
// getnetworkgroupdistribution command.
type NetworkGroupResult struct {
	// Group is the network group key as produced by the address manager's
	// network group rules (for example a /16 IPv4 prefix, a /32 IPv6
	// prefix, "tor:N", "local", or "unroutable").
	Group string `json:"group"`

	// Connections is the total number of connected peers in the group.
	Connections int32 `json:"connections"`

	// Inbound is the number of connected peers in the group that are
	// inbound connections.
	Inbound int32 `json:"inbound"`

	// Outbound is the number of connected peers in the group that are
	// outbound connections.
	Outbound int32 `json:"outbound"`

	// Peers is the sorted list of IDs of the connected peers in the
	// group.
	Peers []int32 `json:"peers"`

	// SyncPeer indicates whether or not the peer currently being used to
	// sync the chain is in this group.
	SyncPeer bool `json:"syncpeer"`

	// Share is the fraction of all connected peers that are in this group
	// (connections divided by the total number of connections).  It is 0
	// when there are no connections.
	Share float64 `json:"share"`
}

// GetNetworkGroupDistributionResult models the data returned from the
// getnetworkgroupdistribution command.  The groups are stably sorted by their
// network group key.  This is a report-only result and never causes peers to
// be disconnected or changes peer selection.
type GetNetworkGroupDistributionResult struct {
	// TotalConnections is the total number of currently connected peers.
	TotalConnections int32 `json:"totalconnections"`

	// InboundConnections is the total number of inbound connections.
	InboundConnections int32 `json:"inboundconnections"`

	// OutboundConnections is the total number of outbound connections.
	OutboundConnections int32 `json:"outboundconnections"`

	// Groups is the per-network-group distribution, stably sorted by the
	// group key.  It is an empty (non-null) array when there are no
	// connections.
	Groups []NetworkGroupResult `json:"groups"`

	// SyncPeerGroup is the network group key of the peer currently being
	// used to sync the chain, or an empty string when there is no sync
	// peer.
	SyncPeerGroup string `json:"syncpeergroup"`

	// LargestGroupShare is the fraction of all connected peers that belong
	// to the network group with the most connections.  It is 0 when there
	// are no connections.
	LargestGroupShare float64 `json:"largestgroupshare"`
}
