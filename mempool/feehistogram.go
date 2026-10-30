// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mempool

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"time"
)

// FeeRateHistogramBucket describes a single bucket of a mempool fee rate
// histogram.
type FeeRateHistogramBucket struct {
	// Count is the number of transactions in the bucket.
	Count uint64

	// VSize is the total virtual size of the transactions in the bucket in
	// virtual bytes.
	VSize int64

	// Fee is the total fee paid by the transactions in the bucket in
	// satoshis.
	Fee int64
}

// FeeRateHistogram describes the fee rate distribution of the transactions in
// the memory pool at SnapshotTime.
type FeeRateHistogram struct {
	// SnapshotTime is the local time the transaction snapshot used to
	// build the histogram was taken.
	SnapshotTime time.Time

	// Boundaries is a copy of the strictly increasing, finite, non-negative
	// fee rate bucket boundaries in satoshis per virtual byte.
	Boundaries []float64

	// Buckets holds the per-bucket statistics.  It contains one entry for
	// each interval between consecutive boundaries, plus an underflow
	// bucket (fee rate below the first boundary) as the first entry and an
	// overflow bucket (fee rate at or above the last boundary) as the last
	// entry, so it always has exactly one more element than Boundaries.
	Buckets []FeeRateHistogramBucket

	// TotalCount is the total number of transactions across all buckets.
	TotalCount uint64

	// TotalVSize is the total virtual size across all buckets in virtual
	// bytes.
	TotalVSize int64

	// TotalFee is the total fee across all buckets in satoshis.
	TotalFee int64
}

// Errors returned when validating fee rate histogram boundaries.
var (
	errNoFeeRateBoundaries = errors.New("at least one fee rate bucket " +
		"boundary must be provided")
	errNonFiniteFeeRateBoundary = errors.New("fee rate bucket boundaries " +
		"must be finite")
	errNegativeFeeRateBoundary = errors.New("fee rate bucket boundaries " +
		"must be non-negative")
	errNonIncreasingFeeRateBoundary = errors.New("fee rate bucket " +
		"boundaries must be strictly increasing")
)

// feeRateBoundary is the exact rational representation num/den of a fee rate
// bucket boundary.  A float64 value is converted to the fraction that exactly
// represents its binary floating point value, which allows the fee rate of a
// transaction (its fee divided by its virtual size) to be compared against the
// boundary with arbitrary-precision integer math instead of truncating the
// rate to an integer or relying on floating point rounding.
type feeRateBoundary struct {
	num *big.Int
	den *big.Int
}

// parseFeeRateBoundaries validates the provided fee rate bucket boundaries and
// returns their exact rational representations.  The boundaries must be
// non-empty, finite (not NaN or +/- infinity), non-negative, and strictly
// increasing.
func parseFeeRateBoundaries(boundaries []float64) ([]feeRateBoundary, error) {
	if len(boundaries) == 0 {
		return nil, errNoFeeRateBoundaries
	}

	parsed := make([]feeRateBoundary, 0, len(boundaries))
	for i, boundary := range boundaries {
		if math.IsNaN(boundary) || math.IsInf(boundary, 0) {
			return nil, fmt.Errorf("%w: boundary #%d is %v",
				errNonFiniteFeeRateBoundary, i, boundary)
		}
		if boundary < 0 {
			return nil, fmt.Errorf("%w: boundary #%d is %v",
				errNegativeFeeRateBoundary, i, boundary)
		}
		if i > 0 && boundary <= boundaries[i-1] {
			return nil, fmt.Errorf("%w: boundary #%d (%v) is not "+
				"greater than boundary #%d (%v)",
				errNonIncreasingFeeRateBoundary, i, boundary,
				i-1, boundaries[i-1])
		}

		// SetFloat64 returns an exact fraction for the float64 value
		// and nil for non-finite values, which have already been
		// rejected above.
		rat := new(big.Rat).SetFloat64(boundary)
		parsed = append(parsed, feeRateBoundary{
			num: new(big.Int).Set(rat.Num()),
			den: new(big.Int).Set(rat.Denom()),
		})
	}

	return parsed, nil
}

// feeRateBucketIndex returns the bucket index of a transaction with the given
// fee and virtual size for the provided parsed boundaries.  The returned index
// ranges from 0 (the underflow bucket) to len(boundaries) (the overflow
// bucket).  The interval at index i for 0 < i < len(boundaries) is
// [boundaries[i-1], boundaries[i]), i.e. boundaries are left-closed and
// right-open.
//
// The fee rate fee/vsize is compared against a boundary num/den exactly via
// cross multiplication (fee*den vs num*vsize) rather than dividing the fee by
// the virtual size first, which would truncate the rate to an integer and
// could misclassify transactions that straddle a boundary.
//
// feeInt, vsizeInt, lhs, and rhs are scratch big.Int values reused across the
// binary search comparisons to avoid allocations.
func feeRateBucketIndex(fee, vsize int64, boundaries []feeRateBoundary,
	feeInt, vsizeInt, lhs, rhs *big.Int) int {

	feeInt.SetInt64(fee)
	vsizeInt.SetInt64(vsize)

	// Find the first boundary that is strictly greater than the
	// transaction fee rate.
	return sort.Search(len(boundaries), func(i int) bool {
		lhs.Mul(feeInt, boundaries[i].den)
		rhs.Mul(boundaries[i].num, vsizeInt)
		return lhs.Cmp(rhs) < 0
	})
}

// FeeRateHistogram returns a histogram of the fee rates paid by the
// transactions currently in the memory pool.
//
// The provided boundaries must be strictly increasing, finite, non-negative fee
// rates in satoshis per virtual byte.  The resulting histogram contains one
// bucket for each interval between consecutive boundaries using left-closed,
// right-open intervals, plus an underflow bucket for transactions below the
// first boundary and an overflow bucket for transactions at or above the last
// boundary.  Each bucket reports the number of transactions it holds, their
// total virtual size, and their total fee.  The histogram also reports the time
// of the transaction snapshot along with the totals across all buckets.
//
// All statistics are computed from a single consistent snapshot of the main
// transaction pool taken under the pool lock, so the totals match the set of
// transactions visible to other pool queries against the same snapshot.
// Orphan transactions are not included.  An empty pool still yields fully
// populated, zero-valued buckets.  This method is read-only: it does not alter
// transaction selection, prioritization, or relay behavior and it does not
// contact any external fee service.
//
// This function is safe for concurrent access.
func (mp *TxPool) FeeRateHistogram(
	boundaries []float64) (*FeeRateHistogram, error) {

	parsed, err := parseFeeRateBoundaries(boundaries)
	if err != nil {
		return nil, err
	}

	hist := &FeeRateHistogram{
		Boundaries: append([]float64(nil), boundaries...),
		Buckets:    make([]FeeRateHistogramBucket, len(parsed)+1),
	}

	// Reusable scratch values for the exact fee rate comparisons.
	var feeInt, vsizeInt, lhs, rhs big.Int

	mp.mtx.RLock()
	hist.SnapshotTime = time.Now()
	for _, desc := range mp.pool {
		fee := desc.Fee
		vsize := GetTxVirtualSize(desc.Tx)

		idx := feeRateBucketIndex(fee, vsize, parsed, &feeInt,
			&vsizeInt, &lhs, &rhs)

		bucket := &hist.Buckets[idx]
		bucket.Count++
		bucket.VSize += vsize
		bucket.Fee += fee

		hist.TotalCount++
		hist.TotalVSize += vsize
		hist.TotalFee += fee
	}
	mp.mtx.RUnlock()

	return hist, nil
}
