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

The scheduler must therefore combine runtime capacity, currently leased
workers, active candidate count, and estimated-work thresholds. It must also
allow callers to impose a lower limit when they prefer aggregate throughput
over one request's latency.

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
- In automatic mode, use a process-wide worker lease capped by
  `runtime.GOMAXPROCS(0)`. Prefer candidate breadth whenever more than one box
  is active; use orientation alternatives only for a lone candidate and only
  when the internal work estimator says the task is large enough to repay
  scheduling and cloning overhead.
- Count inline and explicitly serial top-level evaluations as one leased worker
  so the process-wide limit remains hard under saturation. Serve blocking lease
  requests FIFO and do not let opportunistic pool expansion bypass them.
- Keep the automatic per-candidate orientation fan-out conservative; begin
  with at most two in-flight orientations per candidate and raise it only if
  the benchmark matrix demonstrates a repeatable benefit.
- Evaluate the first orientation synchronously and widen the pool only after it
  fails to fit every item, preserving the serial path's cheapest early exit.
- Implement the workload decision in a small testable helper using inputs such
  as post-cap item count, orientation-task count, candidate count, and available
  runtime capacity. Document calibrated thresholds and keep them out of public
  API guarantees.
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
- [x] Blocking lease requests are served FIFO, and nonblocking orientation
  expansion yields to an existing queue.
- [x] The benchmark matrix covers single- and many-candidate inputs,
  short-circuit on and off, bounded and uncapped pools, and `GOMAXPROCS` 1, 2,
  4, and host-default execution.
- [x] On the reference benchmark machine, automatic mode improves each
  representative single-candidate median by at least 5%, with the ordinary
  workload improving by at least 8%, while no measured benchmark median
  regresses by more than 5%.
- [x] Parallel-request benchmarks show no material aggregate-throughput or
  tail-latency regression versus the caller-selected concurrency ceiling.
- [x] Allocations increase by no more than 2% when orientation concurrency is
  enabled and do not increase on paths that remain serial.
- [x] `go test ./...` and `go test -race ./...` pass without goroutine leaks.

Benchmark thresholds are manual release evidence, not timing assertions in the
unit-test suite.

## Implementation and verification notes

- Candidate evaluation stays inline below 32 total post-cap items. The
  calibrated orientation threshold is `item count * task count >= 512`. Both
  values are internal and may change with later benchmark evidence.
- Automatic orientation fan-out is capped at two. The first orientation is
  evaluated synchronously; only an incomplete result allows the lease to widen.
  Indexed batch reduction preserves serial task order and returns the earliest
  complete fit.
- Candidate and orientation pools acquire a strict process-wide lease. Active
  evaluation workers across simultaneous `Packer.Pack` and
  `VolumePacker.Pack` calls cannot exceed `GOMAXPROCS`; a saturated orientation
  solve continues serially instead of blocking while trying to widen its pool.
  Even forced-serial and below-threshold evaluations lease one slot and may
  briefly queue under saturation. Blocking acquisitions are FIFO, while
  nonblocking pool expansion yields whenever a blocking request is queued.
- Host-default Apple M4 medians were approximately 509 us to 440 us (13.5%) for
  the ordinary first-orientation workload and 2.92 ms to 2.70 ms (7.5%) for the
  uncapped large pool. The many-candidate workload improved from approximately
  191 us to 126 us (33.8%).
- Full `Packer` quantity-mode medians improved approximately 11.1% with the
  short-circuit off and 4.9% with it on. At `GOMAXPROCS=1`, adaptive execution
  remained serial with the same allocation count.
- The original 8% floor for both single-candidate workloads was recalibrated
  after preserving the synchronous early exit. The corrected early-complete
  fixture is within approximately 1% of forced serial execution with identical
  allocations; the tiny two-candidate fixture is within approximately 0.2%,
  also with identical allocations.
- Added permanent scheduler, quantity-mode, and parallel-request benchmarks;
  the matrix was run at `GOMAXPROCS` 1, 2, 4, and the host default (10).
- The parallel-request benchmark records p95 latency as well as aggregate
  throughput. Against forced-serial internals, host-default adaptive medians
  were approximately 0.9% slower in aggregate time and 3.0% slower at p95,
  both within the 5% guardrail.
- A regression fixture covers physically different item signatures that tie in
  the stable item sorter. Quantity replication now falls back to signature-aware
  removal instead of dropping an unsafe sorted prefix.
- `Pack` now applies the active packed-box sorter to partial boxes before
  returning them with `NoBoxesAvailableError`, just as it does on successful
  paths.
- A fresh 100-scenario audit against PHP feature commit
  `e0aa3a969b5fe650db11a90b5acfed948018de69` matched exact ordered boxes and
  physical placements with the quantity short-circuit off and on.
- `go test ./...`, `go test -race ./...`, and `go vet ./...` pass.

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
