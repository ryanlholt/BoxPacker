# PHP 4.x result parity

Status: Complete

## Objective

Advance the completed PHP 3.x-compatible implementation to reproduce the
packing decisions of
`ryanlholt/DvdBoxPacker:feature/quantity-short-circuit` for features shared by
the PHP and Go packages. The preserved 3.x behavior remains available on the
`3.x-parity` branch.

The reference revision for this epic is
`e0aa3a969b5fe650db11a90b5acfed948018de69`.

## Evaluated delta

PHP 4.x special-cases the first item placed into a box by trying every valid
orientation and retaining the best completed packing. PHP 3.x sends that item
through the normal bounded-lookahead orientation sorter once.

Before porting that enhancement, a deterministic 100-case shared-feature
comparison found 95 physical-packing differences and 41 box-count differences.
Temporarily restoring only the exhaustive first-item path reduced ordered,
physical, and box-count differences to zero in all 100 cases, with the quantity
short-circuit both disabled and enabled.

## Stories

1. [S-1: Port the PHP 4.x first-item orientation search](story_01_first_item_orientation.md)
2. [S-2: Add an adaptive bounded evaluation scheduler](story_02_bounded_evaluation_scheduler.md)

## Scope

- Retarget the checked-in PHP golden corpus to the feature branch.
- Verify every golden scenario with the quantity short-circuit off and on.
- Re-run seeded cross-language and short-circuit differential coverage.
- Confirm the additional orientation search does not break bounded
  large-quantity evaluation counts.
- Adaptively schedule independent candidate and first-orientation evaluations
  when the runtime and workload can benefit.

## Acceptance criteria

- [x] The 4.x one-box first-item fixture matches exact PHP placements.
- [x] The checked-in feature-branch corpus matches with the short-circuit off
  and on.
- [x] A deterministic 100-case PHP/Go comparison has zero ordered, physical,
  or box-count differences in both modes.
- [x] Existing short-circuit safety and evaluation-count tests pass.
- [x] `go test ./...` and `go test -race ./...` pass.
- [x] Independent evaluation work uses an adaptive shared concurrency budget
  that improves eligible workloads without changing PHP-compatible results or
  degrading saturated throughput.

## S-1 verification completed

- Regenerated the five-scenario ordered golden corpus from PHP feature commit
  `e0aa3a969b5fe650db11a90b5acfed948018de69` and matched it with the Go
  short-circuit both off and on.
- Deterministic 100-case PHP/Go differential in each short-circuit mode: zero
  ordered, canonical physical-packing, or box-count differences across 200
  scenario runs.
- PHP focused quantity suite: 21 tests, 50 assertions.
- PHP 250-case short-circuit fuzz suite: zero divergences, with capping engaged
  in 249 cases and the lookahead stress region reached in 248 cases.
- Go bounded-evaluation and large-quantity tests passed as part of the full
  suite.
- `go test ./...`
- `go test -race ./...`

## Performance follow-up

S-1 intentionally matched PHP 4.x's more exhaustive first-item search before
optimizing it. S-2 now addresses the independent work exposed by that search.
S-2 implements and verifies that scheduler with deterministic indexed
reduction, runtime/workload-aware bounds, and a serial override.

## S-2 verification completed

- Exact feature-branch golden corpus and a fresh deterministic 100-scenario
  PHP/Go audit in both quantity short-circuit modes.
- Strict process-wide worker-lease, configured-ceiling, tiny-work inline,
  earliest-complete-fit, deterministic-repeat, and synchronous-lookahead tests.
- Permanent single-candidate, many-candidate, bounded/uncapped quantity, and
  parallel-request benchmarks at `GOMAXPROCS` 1, 2, 4, and host default.
- Apple M4 host-default median improvements of approximately 13.5% for the
  ordinary orientation workload, 7.5% for its large uncapped pool, and 33.8%
  for the many-candidate workload after restoring the synchronous early exit.
- Tiny-candidate and early-complete regressions are approximately 0.2% and 1%,
  respectively, with allocation counts identical to forced-serial execution.
- Saturated parallel-request aggregate time and p95 latency were approximately
  0.9% and 3.0% slower than forced-serial internals, within the 5% guardrail.
- Quantity replication has a regression fixture for physically different
  signatures that tie in the stable item sorter; packed and unpacked quantities
  are preserved.
- `go test ./...`, `go test -race ./...`, and `go vet ./...`.

## Out of scope

- PHP constrained-placement callbacks.
- Linked-item groups.
- Strict input ordering.
- Timeout checking.
- `packAllPermutations`.
- Replacing the greedy algorithm with a globally optimal solver.
