# Packing correctness and scale improvements

Implemented on `improve/packing-correctness-and-scale`, based on
`c6ab30c8c65264fd638cfbdc860f140c5aad18a7`.

## Behavior

- A preceding item's orientation is reused only when allowed by the current
  item's rotation policy. Equal-sized items with different allowed orientation
  sets are no longer skipped together.
- Repeated identities use compact quantity entries throughout sorting, cloning,
  skipped-item queues, capping, and removal. Distinct objects retain their own
  identities and stable input order. Identity removal is one stable pass over
  entries rather than a scan and slice shift for each placement.
- Replicas consume actual input identities and own independent placement records.
  Physically different signatures tied by the item sorter disable replication,
  because their interleaved order cannot be inferred from counts.
- Stability and lookahead caches use FIFO eviction, retaining at most 1,024 and
  4,096 entries respectively. Box ordering is computed once per packer, redundant
  square-box rotations are skipped, and scheduling estimates use compact entries
  plus possible placements instead of raw quantity.
- `SetPackingSearchBudget(n)` enables bounded subset-layout search for complete
  orders of at most 16 items with the default box sorter. It respects original
  supply and accepts only fewer boxes or less total outer volume at equal count.
  It is off by default. Larger orders, partial results, and custom sorters skip it.

The optional search changes the known widths `6,5,3,2,2,2` example from three
`10×1×1` boxes to two: `[6,2,2]` and `[5,3,2]`. It limits additional single-box
solves to the supplied budget, and both screening and coverage traversal to
`64 * min(budget, 4096)` steps each. It does not prove global optimality.
Post-pack weight redistribution can change the outer-volume objective; callers
can disable redistribution when that objective takes priority.

## Verification

The existing golden PHP corpus remains unchanged and passes. New checks cover
mixed rotation policies, exact item conservation, independent placement ownership,
stable ties, clone isolation, bounded concurrent cache eviction, limited supply,
search budgets, and search exclusion rules. A seeded 150-case packing test checks
bounds, overlaps, weight, rotation, and identities; 40 tiny packing cases compare
the optional search against an independent exhaustive one-dimensional oracle.

The gap-analysis harness no longer treats exhaustive search of a limited pattern
pool as a geometric optimality proof. Its SKU priority orders now create different
bounded demand vectors. Only matching an independent lower bound proves a global
box-count optimum. A regression demonstrates a completed restricted pattern
search whose result exceeds an independently feasible optimum.

Run:

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -bench 'Benchmark(LargeQuantity|LargeMixedQuantity|AdaptiveFirstOrientationScheduler|AdaptiveQuantityModes|CompactQuantityInput|PackingSearchSmallOrder)$' -benchmem -benchtime=300ms -count=3
go test -run '^$' -bench '^BenchmarkPlacementStrategies$' -benchmem -benchtime=300ms -count=3
```

## Measured performance

Apple M4, darwin/arm64, Go 1.27.1, runtime concurrency 10, 2026-10-02. Values
are medians of three runs, including construction and packing. The baseline
used 200 ms benchmark intervals; the final implementation used 300 ms. These
are steady-state microbenchmarks with normal benchmark warm-up, not cold-cache
measurements or service latency guarantees. MB below means allocated bytes
per operation divided by 1,000,000, not retained heap or process memory.

| Workload | Before | After | Runtime reduction | Allocated MB before → after |
|---|---:|---:|---:|---:|
| 100,000 repeated items, short-circuit on | 11.981 ms | 4.331 ms | 63.9% | 13.360 → 9.223 |
| 9,000 mixed items, short-circuit on | 2.698 ms | 0.379 ms | 86.0% | 2.373 → 0.865 |
| Ordinary orientation workload, adaptive | 0.424 ms | 0.168 ms | 60.3% | 2.125 → 0.389 |
| Large orientation pool, adaptive | 2.478 ms | 0.172 ms | 93.1% | 19.922 → 0.406 |
| Quantity-mode workload, short-circuit off, adaptive | 3.930 ms | 1.795 ms | 54.3% | 17.214 → 3.843 |
| Quantity-mode workload, short-circuit on, adaptive | 2.529 ms | 1.432 ms | 43.4% | 9.018 → 3.085 |

The 100,000-item workload's allocation count increased from 50,547 to 75,309
because replicas now own placement records. Total allocated bytes decreased
31%, and runtime decreased despite correcting that ownership defect. Compact
input insertion, cloning, and signature counting allocated 672 bytes at each
tested quantity: 1,000, 100,000, and 1,000,000. Output still requires one placement
per physical item. Distinct input identities still require distinct entries.

The six-item search example took approximately 5.6 µs without search and
47.6 µs with a budget of 64, reducing box count from three to two. This is why
search remains opt-in and small-order only.

## Alternate placement experiment

`extreme_point_benchmark_test.go` contains a test-only exposed-corner and axis
projection prototype. It is an exploratory placement strategy, not a complete
implementation of the published extreme-point algorithms. For background, see
[Crainic, Perboli, and Tadei](https://www.cirrelt.ca/documentstravail/cirrelt-2007-41.pdf).

| Single-box fixture | Layers | Prototype | Items packed, layers → prototype | Utilization, layers → prototype |
|---|---:|---:|---:|---:|
| Mixed, weight-limited | 173.1 µs | 27.3 µs | 7 → 9 | 89.2% → 82.5% |
| Awkward shapes | 88.1 µs | 13.2 µs | 8 → 8 | 77.1% → 77.1% |

These promising results cover only two fixtures. The prototype validates
geometric bounds, collision avoidance, allowed rotations, and weight, but does
not reproduce the production stability heuristics or PHP layouts. More items
at lower volume also demonstrates that placement objectives can disagree.
It remains benchmark-only; replacing the default solver needs broader quality,
stability, supply, and whole-order evaluation.
