# S-2: Add an adaptive bounded evaluation scheduler

Status: Complete

## Goal

Reduce single-order latency when runtime capacity and workload size justify
concurrency, while preserving exact PHP-compatible results, deterministic
tie-breaking, bounded resource use, and throughput under concurrent load.

## Evidence

Before this story, `Packer.packBasic` started one goroutine per candidate box
type without a ceiling, while each `volumePacker` evaluated up to two box-width
permutations times six valid first-item orientations serially. One-box
benchmarks therefore left most CPU capacity unused, while many-box inputs could
create more goroutines than the runtime could profitably execute.

A temporary Apple M4 prototype compared the current serial orientation loop
with a result-equivalent worker pool:

| Workload | Serial | 2 workers | 4 workers | 10 workers |
|---|---:|---:|---:|---:|
| Ordinary bounded pool | ~475 us | ~397 us | ~466 us | ~518 us |
| Large uncapped pool | ~2.95 ms | ~2.68 ms | ~3.69 ms | ~4.50 ms |

Two workers improved the sampled single-candidate workloads by roughly 10-17%.
Larger worker pools regressed because concurrent item-list cloning and
traversal increased allocation and memory-bandwidth pressure. These results
are evidence that concurrency can help, not evidence that two workers are
universally optimal.

## Environment and workload sensitivity

The useful concurrency level depends on:

- `GOMAXPROCS`, container CPU quotas, and available cores.
- How many candidate boxes already run concurrently.
- The number of distinct first-item alternatives.
- Item-pool size after quantity capping and whether an early orientation fits
  everything.
- Memory bandwidth, cache size, allocator behavior, `GOGC`, and `GOMEMLIMIT`.
- Whether the host is processing one pack or many independent packs at once.
- The cost and synchronization behavior of custom `Box` and `Item` getters.

The scheduler must therefore combine runtime capacity, active candidate count,
and an estimated-work threshold. It must also allow callers to impose a lower
limit when they prefer aggregate throughput over one request's latency.

## Scope

- Build indexed evaluation tasks at the useful breadth for the current
  iteration: candidate-box tasks when multiple candidates are active, or
  box-width/first-item-orientation tasks when there is only one candidate.
- Execute tasks through at most one worker pool per packing iteration. Candidate
  workers run their volume evaluation serially, so candidate and orientation
  pools are never nested.
- Add `SetMaxConcurrency` to `Packer` and `VolumePacker` with these semantics:
  `0` selects automatic behavior, `1` forces serial evaluation, and values
  greater than one are hard per-instance ceilings rather than target worker
  counts.
- In automatic mode, use `runtime.GOMAXPROCS(0)` as the upper bound and divide
  that capacity among active pack calls. Prefer candidate breadth whenever more
  than one box is active; use orientation alternatives only for a lone
  candidate and only when the internal work estimator says the task is large
  enough to repay scheduling and cloning overhead.
- Keep the automatic per-candidate orientation fan-out conservative; begin
  with at most two in-flight orientations per candidate and raise it only if
  the benchmark matrix demonstrates a repeatable benefit.
- Implement the workload decision in a small testable helper using inputs such
  as post-cap item count, orientation-task count, active pack-call count, and
  available runtime capacity. Document any calibrated threshold and keep it out
  of public API guarantees.
- Give every task its own cloned item list and packing state. Box and item
  objects remain shared for read-only access, matching the package's existing
  candidate-evaluation contract.
- Reduce orientation results by their original indices so the first complete
  fit, utilization ties, box-rotation order, and candidate ordering match the
  current serial/PHP behavior exactly.
- Keep recursive single-pass lookahead packing synchronous; it must not submit
  nested scheduler work.
- Use the same adaptive bounded orientation path for public
  `VolumePacker.Pack`.
- Add permanent benchmarks for one-candidate, many-candidate, bounded, and
  uncapped inputs at `GOMAXPROCS` 1, 2, 4, and the host default.
- Add parallel-request benchmarks to measure aggregate throughput and tail
  behavior when callers already run independent `Packer` instances
  concurrently.
- Document the internal concurrency boundary and the requirement that custom
  `Box` and `Item` implementations support concurrent read access.

## Acceptance criteria

- [x] The feature-branch golden corpus matches exactly with the quantity
  short-circuit off and on.
- [x] The deterministic 100-case PHP/Go corpus has zero ordered, physical, or
  box-count differences in both modes.
- [x] Indexed result reduction is deterministic across repeated runs and
  preserves the earliest complete-fit result.
- [x] `SetMaxConcurrency(1)` is fully serial, `SetMaxConcurrency(0)` is
  adaptive, and positive limits are never exceeded.
- [x] Automatic mode remains serial when `GOMAXPROCS` is one or the estimated
  work is below the calibrated threshold.
- [x] Tests prove the shared worker count and per-candidate orientation count
  never exceed their budgets, and single-pass lookahead never recursively
  submits work.
- [x] The benchmark matrix covers single- and many-candidate inputs,
  short-circuit on and off, bounded and uncapped pools, and `GOMAXPROCS` 1, 2,
  4, and host-default execution.
- [x] On the reference benchmark machine, automatic mode improves both
  representative single-candidate medians by at least 8% while no measured
  benchmark median regresses by more than 5%.
- [x] Parallel-request benchmarks show no material aggregate-throughput or
  tail-latency regression versus the caller-selected concurrency ceiling.
- [x] Allocations increase by no more than 2% when orientation concurrency is
  enabled and do not increase on paths that remain serial.
- [x] `go test ./...` and `go test -race ./...` pass without goroutine leaks.

Benchmark thresholds are manual release evidence, not timing assertions in the
unit-test suite.

## Implementation and verification notes

- The calibrated orientation threshold is `item count * task count >= 512`.
  It is internal and may change with later benchmark evidence.
- Automatic orientation fan-out is capped at two. Indexed batch reduction
  preserves the serial task order and returns the earliest complete fit.
- Active public `Packer.Pack` and `VolumePacker.Pack` calls share the runtime
  capacity estimate. On the Apple M4 reference host, this reduced the saturated
  parallel-request comparison from an approximately 8% regression to about 1%.
- Host-default median results were approximately 501 us to 397 us (21%) for the
  ordinary first-orientation workload and 2.81 ms to 2.47 ms (12%) for the
  uncapped large pool. The many-candidate workload improved from approximately
  189 us to 126 us (33%).
- Full `Packer` quantity-mode medians improved approximately 15% with the
  short-circuit off and 6% with it on. At `GOMAXPROCS=1`, adaptive execution
  remained serial with the same allocation count.
- Added permanent scheduler, quantity-mode, and parallel-request benchmarks;
  the matrix was run at `GOMAXPROCS` 1, 2, 4, and the host default (10).
- The parallel-request benchmark records p95 latency as well as aggregate
  throughput. Host-default medians were effectively neutral on throughput and
  approximately 2% better for adaptive p95 latency than forced-serial internals.
- A fresh 100-scenario audit against PHP feature commit
  `e0aa3a969b5fe650db11a90b5acfed948018de69` matched exact ordered boxes and
  physical placements with the quantity short-circuit off and on.
- `go test ./...` and `go test -race ./...` pass.

## Out of scope

- Parallelizing placement within a layer.
- Speculative weight-redistribution moves.
- Parallel box replication or output construction.
- Batch scheduling across independent `Packer` instances; callers can already
  run separate packers in their own worker pool.
- Allocation or compact-quantity representation changes unrelated to the
  scheduler.

## Dependencies

- S-1
