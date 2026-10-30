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

// GetMempoolFeeRateHistogramBucketResult models a single bucket of the data
// returned from the getmempoolfeeratehistogram command.
type GetMempoolFeeRateHistogramBucketResult struct {
	// FeeRateFloor is the inclusive lower fee rate boundary of the bucket
	// in satoshis per virtual byte.  It is null for the underflow bucket
	// that holds transactions with fee rates below the first boundary.
	FeeRateFloor *float64 `json:"feerate_floor"`

	// FeeRateCeiling is the exclusive upper fee rate boundary of the bucket
	// in satoshis per virtual byte.  It is null for the overflow bucket
	// that holds transactions with fee rates at or above the last boundary.
	FeeRateCeiling *float64 `json:"feerate_ceiling"`

	// Count is the number of transactions in the bucket.
	Count uint64 `json:"count"`

	// VSize is the total virtual size of the transactions in the bucket in
	// virtual bytes.
	VSize int64 `json:"vsize"`

	// Fee is the total fee paid by the transactions in the bucket in
	// satoshis.
	Fee int64 `json:"fee"`
}

// GetMempoolFeeRateHistogramResult models the data returned from the
// getmempoolfeeratehistogram command.
type GetMempoolFeeRateHistogramResult struct {
	// SnapshotTime is the local time the transaction snapshot used to
	// build the histogram was taken, in seconds since 1 Jan 1970 GMT.
	SnapshotTime int64 `json:"snapshot_time"`

	// Buckets holds the per-bucket statistics.  It contains one entry for
	// each interval between consecutive caller-provided boundaries, plus an
	// underflow bucket (fee rate below the first boundary) as the first
	// entry and an overflow bucket (fee rate at or above the last boundary)
	// as the last entry.
	Buckets []GetMempoolFeeRateHistogramBucketResult `json:"buckets"`

	// TotalCount is the total number of transactions across all buckets.
	TotalCount uint64 `json:"total_count"`

	// TotalVSize is the total virtual size across all buckets in virtual
	// bytes.
	TotalVSize int64 `json:"total_vsize"`

	// TotalFee is the total fee across all buckets in satoshis.
	TotalFee int64 `json:"total_fee"`
}
