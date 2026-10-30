// Copyright (c) 2016-2017 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcjson

// PingRTTDistributionBucketResult models a single histogram bucket returned
// by the getpingrttdistribution command.  Each bucket covers the half-open
// microsecond range [LowerInclusive, UpperExclusive).  A nil LowerInclusive
// identifies the underflow bucket for values below the first requested
// boundary, while a nil UpperExclusive identifies the overflow bucket for
// values greater than or equal to the last requested boundary.
type PingRTTDistributionBucketResult struct {
	LowerInclusive *uint64 `json:"lower_inclusive"`
	UpperExclusive *uint64 `json:"upper_exclusive"`
	Inbound        uint32  `json:"inbound"`
	Outbound       uint32  `json:"outbound"`
}

// GetPingRTTDistributionResult models the data returned from the
// getpingrttdistribution command.
type GetPingRTTDistributionResult struct {
	// SampleTime is the local time the connected peer set was sampled, in
	// seconds since 1 Jan 1970 GMT.
	SampleTime int64 `json:"sample_time"`

	// TotalPeers is the number of connected peers that were sampled.
	TotalPeers uint32 `json:"total_peers"`

	// NeverCompletedPing is the number of peers for which a pong matching
	// a sent ping nonce has never been received.
	NeverCompletedPing uint32 `json:"never_completed_ping"`

	// PingPending is the number of peers with a ping that is still waiting
	// for its matching pong at the time the peer was sampled.
	PingPending uint32 `json:"ping_pending"`

	// Buckets holds the per-direction peer counts for each half-open
	// microsecond range, including the overflow buckets.
	Buckets []PingRTTDistributionBucketResult `json:"buckets"`
}

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
