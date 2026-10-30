// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mempool

import (
	"errors"
	"math"
	"math/big"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
)

// newHistogramTestTx returns a unique minimal, non-segwit transaction with the
// requested number of inputs and outputs.  Each generated transaction uses a
// unique previous outpoint index so the resulting transactions have distinct
// hashes and can coexist in the same test pool.
func newHistogramTestTx(uniqueInput uint32, numInputs,
	numOutputs int) *btcutil.Tx {

	tx := wire.NewMsgTx(wire.TxVersion)
	for i := 0; i < numInputs; i++ {
		tx.AddTxIn(&wire.TxIn{
			PreviousOutPoint: wire.OutPoint{
				Hash:  chainhash.Hash{byte(uniqueInput)},
				Index: uniqueInput + uint32(i),
			},
			Sequence: wire.MaxTxInSequenceNum,
		})
	}
	for i := 0; i < numOutputs; i++ {
		tx.AddTxOut(&wire.TxOut{})
	}
	return btcutil.NewTx(tx)
}

// TestParseFeeRateBoundaries ensures the fee rate bucket boundary validation
// behaves as expected, including rejection of empty, non-finite, negative, and
// non-increasing inputs.
func TestParseFeeRateBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		boundaries []float64
		wantErr    error
	}{
		{
			name:       "nil boundaries",
			boundaries: nil,
			wantErr:    errNoFeeRateBoundaries,
		},
		{
			name:       "empty boundaries",
			boundaries: []float64{},
			wantErr:    errNoFeeRateBoundaries,
		},
		{
			name:       "NaN boundary",
			boundaries: []float64{1, math.NaN()},
			wantErr:    errNonFiniteFeeRateBoundary,
		},
		{
			name:       "positive infinity boundary",
			boundaries: []float64{1, math.Inf(1)},
			wantErr:    errNonFiniteFeeRateBoundary,
		},
		{
			name:       "negative infinity boundary",
			boundaries: []float64{math.Inf(-1), 1},
			wantErr:    errNonFiniteFeeRateBoundary,
		},
		{
			name:       "negative boundary",
			boundaries: []float64{-0.1, 1},
			wantErr:    errNegativeFeeRateBoundary,
		},
		{
			name:       "equal boundaries",
			boundaries: []float64{1, 1},
			wantErr:    errNonIncreasingFeeRateBoundary,
		},
		{
			name:       "decreasing boundaries",
			boundaries: []float64{5, 1},
			wantErr:    errNonIncreasingFeeRateBoundary,
		},
		{
			name:       "duplicate zero boundaries",
			boundaries: []float64{0, 0},
			wantErr:    errNonIncreasingFeeRateBoundary,
		},
		{
			name:       "single boundary",
			boundaries: []float64{1},
			wantErr:    nil,
		},
		{
			name:       "negative zero first boundary",
			boundaries: []float64{math.Copysign(0, -1), 1},
			wantErr:    nil,
		},
		{
			name:       "valid boundaries",
			boundaries: []float64{0, 1.5, 100, 1000.25},
			wantErr:    nil,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := parseFeeRateBoundaries(test.boundaries)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("parseFeeRateBoundaries: expected error %v, got %v",
						test.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFeeRateBoundaries: unexpected error: %v", err)
			}
			if len(parsed) != len(test.boundaries) {
				t.Fatalf("parsed %d boundaries, want %d",
					len(parsed), len(test.boundaries))
			}
		})
	}
}

// newBucketIndexScratch returns fresh scratch big.Int values for the exact fee
// rate comparisons.  Each parallel test needs its own values.
func newBucketIndexScratch() (feeInt, vsizeInt, lhs, rhs *big.Int) {
	return new(big.Int), new(big.Int), new(big.Int), new(big.Int)
}

// TestFeeRateBucketIndex ensures transactions are placed into the expected
// buckets using left-closed, right-open intervals, including exact boundary
// matches.
func TestFeeRateBucketIndex(t *testing.T) {
	t.Parallel()

	parsed, err := parseFeeRateBoundaries([]float64{1, 5, 20})
	if err != nil {
		t.Fatalf("parseFeeRateBoundaries: unexpected error: %v", err)
	}

	const vsize int64 = 100
	tests := []struct {
		name string
		fee  int64
		want int
	}{
		{name: "zero fee", fee: 0, want: 0},
		{name: "just below first boundary", fee: vsize*1 - 1, want: 0},
		{name: "exactly first boundary", fee: vsize * 1, want: 1},
		{name: "just below middle boundary", fee: vsize*5 - 1, want: 1},
		{name: "exactly middle boundary", fee: vsize * 5, want: 2},
		{name: "just below last boundary", fee: vsize*20 - 1, want: 2},
		{name: "exactly last boundary is overflow", fee: vsize * 20, want: 3},
		{name: "above last boundary is overflow", fee: vsize*20 + 1, want: 3},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			feeInt, vsizeInt, lhs, rhs := newBucketIndexScratch()
			got := feeRateBucketIndex(test.fee, vsize, parsed,
				feeInt, vsizeInt, lhs, rhs)
			if got != test.want {
				t.Fatalf("feeRateBucketIndex fee=%d: got %d, want %d",
					test.fee, got, test.want)
			}
		})
	}
}

// TestFeeRateBucketIndexExactComparison ensures the fee rate is compared
// against the boundary without truncating the rate or relying on floating
// point rounding that could misclassify boundary transactions.
func TestFeeRateBucketIndexExactComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		boundaries []float64
		fee        int64
		vsize      int64
		want       int
	}{{
		// The float64 value 0.1 is slightly greater than exactly 1/10,
		// so a transaction paying exactly 1 satoshi per 10 virtual
		// bytes has a fee rate just below the boundary and must land in
		// the underflow bucket.  Naive float64 cross multiplication
		// rounds 0.1*10 to 1 and would place it in the upper bucket.
		name:       "exact tenth below float64 boundary",
		boundaries: []float64{0.1},
		fee:        1,
		vsize:      10,
		want:       0,
	}, {
		// The float64 value nearest to 1/3 is slightly less than 1/3,
		// so a transaction paying 1 satoshi per 3 virtual bytes must
		// land at or above the boundary.  Integer fee rate truncation
		// yields zero and would place it in the underflow bucket.
		name:       "one third above truncated integer rate",
		boundaries: []float64{1.0 / 3.0},
		fee:        1,
		vsize:      3,
		want:       1,
	}, {
		// A zero boundary is inclusive, so a zero-fee transaction is at
		// the boundary rather than below it.
		name:       "zero fee against zero boundary",
		boundaries: []float64{0, 1},
		fee:        0,
		vsize:      1,
		want:       1,
	}, {
		// Large values must not suffer precision loss: the fee is
		// exactly one satoshi below the boundary product.
		name:       "large fee just below boundary",
		boundaries: []float64{100000},
		fee:        100000*1_000_000 - 1,
		vsize:      1_000_000,
		want:       0,
	}, {
		// The corresponding fee exactly on the large boundary lands
		// in the overflow bucket because the last boundary is
		// inclusive.
		name:       "large fee exactly on boundary",
		boundaries: []float64{100000},
		fee:        100000 * 1_000_000,
		vsize:      1_000_000,
		want:       1,
	}}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := parseFeeRateBoundaries(test.boundaries)
			if err != nil {
				t.Fatalf("parseFeeRateBoundaries: unexpected error: %v",
					err)
			}

			feeInt, vsizeInt, lhs, rhs := newBucketIndexScratch()
			got := feeRateBucketIndex(test.fee, test.vsize, parsed,
				feeInt, vsizeInt, lhs, rhs)
			if got != test.want {
				t.Fatalf("feeRateBucketIndex: got %d, want %d",
					got, test.want)
			}
		})
	}
}

// TestFeeRateHistogramEmptyPool ensures an empty pool still returns fully
// populated, zero-valued buckets along with a snapshot time.
func TestFeeRateHistogramEmptyPool(t *testing.T) {
	t.Parallel()

	pool := &TxPool{
		pool:    make(map[chainhash.Hash]*TxDesc),
		orphans: make(map[chainhash.Hash]*orphanTx),
	}

	boundaries := []float64{1, 5, 20}
	before := time.Now()
	hist, err := pool.FeeRateHistogram(boundaries)
	after := time.Now()
	if err != nil {
		t.Fatalf("FeeRateHistogram: unexpected error: %v", err)
	}

	if len(hist.Buckets) != len(boundaries)+1 {
		t.Fatalf("got %d buckets, want %d", len(hist.Buckets),
			len(boundaries)+1)
	}
	for i, bucket := range hist.Buckets {
		if bucket.Count != 0 || bucket.VSize != 0 || bucket.Fee != 0 {
			t.Fatalf("bucket %d is not zero-valued: %+v", i, bucket)
		}
	}
	if hist.TotalCount != 0 || hist.TotalVSize != 0 || hist.TotalFee != 0 {
		t.Fatalf("totals must be zero for an empty pool, got %+v", hist)
	}
	if hist.SnapshotTime.Before(before) || hist.SnapshotTime.After(after) {
		t.Fatalf("snapshot time %v is outside [%v, %v]",
			hist.SnapshotTime, before, after)
	}

	// The returned boundaries must be a copy of the caller slice.
	if len(hist.Boundaries) != len(boundaries) {
		t.Fatalf("got %d boundaries, want %d", len(hist.Boundaries),
			len(boundaries))
	}
	boundaries[0] = 999
	if hist.Boundaries[0] == 999 {
		t.Fatal("returned boundaries must not alias the caller slice")
	}
}

// TestFeeRateHistogramInvalidBoundaries ensures invalid boundaries result in
// an error.
func TestFeeRateHistogramInvalidBoundaries(t *testing.T) {
	t.Parallel()

	pool := &TxPool{pool: make(map[chainhash.Hash]*TxDesc)}
	if _, err := pool.FeeRateHistogram(nil); err == nil {
		t.Fatal("FeeRateHistogram: expected error for nil boundaries")
	}
	if _, err := pool.FeeRateHistogram([]float64{2, 1}); err == nil {
		t.Fatal("FeeRateHistogram: expected error for decreasing boundaries")
	}
}

// TestFeeRateHistogramStats ensures histogram bucket and total statistics are
// computed correctly from a single pool snapshot, that exact boundary matches
// follow left-closed, right-open semantics, and that orphans are excluded.
func TestFeeRateHistogramStats(t *testing.T) {
	t.Parallel()

	boundaries := []float64{1, 5, 20}

	// Each entry selects a transaction shape and a fee expressed relative
	// to its measured virtual size so the expected fee rate is exact.
	entries := []struct {
		numInputs, numOutputs int
		rateVbytes            int64 // exact sat/vbyte target
		delta                 int64 // fee adjustment from the target
		wantBucket            int
	}{
		{1, 1, 0, 0, 0},     // zero fee
		{1, 2, 1, -1, 0},    // just below 1 sat/vbyte
		{1, 1, 1, 0, 1},     // exactly 1 sat/vbyte
		{2, 1, 5, -1, 1},    // just below 5 sat/vbyte
		{1, 1, 5, 0, 2},     // exactly 5 sat/vbyte
		{1, 3, 20, -1, 2},   // just below 20 sat/vbyte
		{1, 1, 20, 0, 3},    // exactly 20 sat/vbyte, overflow
		{3, 1, 20, 1000, 3}, // above 20 sat/vbyte, overflow
	}

	pool := &TxPool{
		pool:    make(map[chainhash.Hash]*TxDesc, len(entries)),
		orphans: make(map[chainhash.Hash]*orphanTx),
	}

	wantBuckets := make([]FeeRateHistogramBucket, len(boundaries)+1)
	var wantTotalCount uint64
	var wantTotalVSize, wantTotalFee int64
	for i, entry := range entries {
		tx := newHistogramTestTx(uint32(i)+1, entry.numInputs,
			entry.numOutputs)
		vsize := GetTxVirtualSize(tx)
		fee := entry.rateVbytes*vsize + entry.delta

		desc := &TxDesc{}
		desc.Tx = tx
		desc.Fee = fee
		pool.pool[*tx.Hash()] = desc

		wantBuckets[entry.wantBucket].Count++
		wantBuckets[entry.wantBucket].VSize += vsize
		wantBuckets[entry.wantBucket].Fee += fee
		wantTotalCount++
		wantTotalVSize += vsize
		wantTotalFee += fee
	}

	// Add an orphan with a large fee.  It must never be counted.
	orphan := newHistogramTestTx(250, 1, 1)
	pool.orphans[*orphan.Hash()] = &orphanTx{tx: orphan}

	hist, err := pool.FeeRateHistogram(boundaries)
	if err != nil {
		t.Fatalf("FeeRateHistogram: unexpected error: %v", err)
	}

	if len(hist.Buckets) != len(boundaries)+1 {
		t.Fatalf("got %d buckets, want %d", len(hist.Buckets),
			len(boundaries)+1)
	}
	if !reflect.DeepEqual(hist.Buckets, wantBuckets) {
		t.Fatalf("bucket stats mismatch:\n got: %+v\nwant: %+v",
			hist.Buckets, wantBuckets)
	}
	if hist.TotalCount != wantTotalCount {
		t.Fatalf("TotalCount: got %d, want %d", hist.TotalCount,
			wantTotalCount)
	}
	if hist.TotalVSize != wantTotalVSize {
		t.Fatalf("TotalVSize: got %d, want %d", hist.TotalVSize,
			wantTotalVSize)
	}
	if hist.TotalFee != wantTotalFee {
		t.Fatalf("TotalFee: got %d, want %d", hist.TotalFee, wantTotalFee)
	}
	if hist.TotalCount != uint64(pool.Count()) {
		t.Fatalf("TotalCount %d does not match pool Count %d",
			hist.TotalCount, pool.Count())
	}
	if hist.SnapshotTime.IsZero() {
		t.Fatal("snapshot time must be set")
	}
	if !reflect.DeepEqual(hist.Boundaries, boundaries) {
		t.Fatalf("boundaries mismatch: got %v, want %v",
			hist.Boundaries, boundaries)
	}

	// The sum of the bucket stats must equal the overall totals.
	var bucketCount uint64
	var bucketVSize, bucketFee int64
	for _, bucket := range hist.Buckets {
		bucketCount += bucket.Count
		bucketVSize += bucket.VSize
		bucketFee += bucket.Fee
	}
	if bucketCount != hist.TotalCount || bucketVSize != hist.TotalVSize ||
		bucketFee != hist.TotalFee {
		t.Fatalf("sum of bucket stats (%d, %d, %d) does not match totals (%d, %d, %d)",
			bucketCount, bucketVSize, bucketFee,
			hist.TotalCount, hist.TotalVSize, hist.TotalFee)
	}
}

// independentBucketIndex classifies a transaction into a bucket using
// arbitrary-precision rational comparison.  It intentionally reimplements the
// classification independently so the real pool test does not simply compare
// the implementation against itself.
func independentBucketIndex(fee, vsize int64, boundaries []float64) int {
	rate := new(big.Rat).SetFrac(big.NewInt(fee), big.NewInt(vsize))
	return sort.Search(len(boundaries), func(i int) bool {
		return rate.Cmp(new(big.Rat).SetFloat64(boundaries[i])) < 0
	})
}

// TestFeeRateHistogramRealPool exercises FeeRateHistogram against transactions
// accepted through the full mempool flow and ensures the result is consistent
// with the independently enumerated transaction descriptors, orphans are
// excluded, and every transaction is accounted for exactly once.
func TestFeeRateHistogramRealPool(t *testing.T) {
	t.Parallel()

	harness, spendableOuts, err := newPoolHarness(&chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("unable to create test pool: %v", err)
	}

	// Create a second coinbase with multiple outputs to provide enough
	// spendable outputs for several transactions.
	ctx := &testContext{t: t, harness: harness}
	coinbase := ctx.addCoinbaseTx(4)
	extraOuts := make([]spendableOutput, 0, 4)
	for i := uint32(0); i < 4; i++ {
		extraOuts = append(extraOuts, txOutToSpendableOut(coinbase, i))
	}
	allOuts := append(spendableOuts, extraOuts...)

	fees := []btcutil.Amount{0, 250, 1500, 6000, 40000}
	for i, fee := range fees {
		ctx.addSignedTx([]spendableOutput{allOuts[i]}, 1, fee,
			false, false)
	}

	// Submit an orphan transaction whose parent is not available.  Use an
	// otherwise unspent output to root the chain so the only reason the
	// transaction is held is its missing parent.  It must be tracked by
	// the pool but excluded from the histogram.
	orphanCoinbase := ctx.addCoinbaseTx(1)
	orphanRoot := txOutToSpendableOut(orphanCoinbase, 0)
	orphanChain, err := harness.CreateTxChain(orphanRoot, 2)
	if err != nil {
		t.Fatalf("unable to create orphan chain: %v", err)
	}
	if _, err := harness.txPool.ProcessTransaction(orphanChain[1], true,
		false, 0); err != nil {
		t.Fatalf("unable to process orphan: %v", err)
	}
	if !harness.txPool.IsOrphanInPool(orphanChain[1].Hash()) {
		t.Fatal("test transaction is not in the orphan pool")
	}

	boundaries := []float64{1, 10, 100}
	hist, err := harness.txPool.FeeRateHistogram(boundaries)
	if err != nil {
		t.Fatalf("FeeRateHistogram: unexpected error: %v", err)
	}

	// Independently enumerate the same statistics from the pool
	// descriptors and classify every transaction using a separate
	// implementation.
	descs := harness.txPool.TxDescs()
	if int(hist.TotalCount) != len(descs) {
		t.Fatalf("TotalCount %d does not match %d transaction descriptors",
			hist.TotalCount, len(descs))
	}
	if int(hist.TotalCount) != harness.txPool.Count() {
		t.Fatalf("TotalCount %d does not match pool Count %d",
			hist.TotalCount, harness.txPool.Count())
	}

	wantBuckets := make([]FeeRateHistogramBucket, len(boundaries)+1)
	var wantVSize, wantFee int64
	for _, desc := range descs {
		vsize := GetTxVirtualSize(desc.Tx)
		idx := independentBucketIndex(desc.Fee, vsize, boundaries)
		wantBuckets[idx].Count++
		wantBuckets[idx].VSize += vsize
		wantBuckets[idx].Fee += desc.Fee
		wantVSize += vsize
		wantFee += desc.Fee
	}

	if !reflect.DeepEqual(hist.Buckets, wantBuckets) {
		t.Fatalf("bucket stats mismatch:\n got: %+v\nwant: %+v",
			hist.Buckets, wantBuckets)
	}
	if hist.TotalVSize != wantVSize {
		t.Fatalf("TotalVSize: got %d, want %d", hist.TotalVSize,
			wantVSize)
	}
	if hist.TotalFee != wantFee {
		t.Fatalf("TotalFee: got %d, want %d", hist.TotalFee, wantFee)
	}
	if hist.SnapshotTime.IsZero() {
		t.Fatal("snapshot time must be set")
	}
}
