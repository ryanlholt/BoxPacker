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

## Scope

1. [S-1: Port the PHP 4.x first-item orientation search](story_01_first_item_orientation.md)
2. Retarget the checked-in PHP golden corpus to the feature branch.
3. Verify every golden scenario with the quantity short-circuit off and on.
4. Re-run seeded cross-language and short-circuit differential coverage.
5. Confirm the additional orientation search does not break bounded
   large-quantity evaluation counts.

## Acceptance criteria

- [x] The 4.x one-box first-item fixture matches exact PHP placements.
- [x] The checked-in feature-branch corpus matches with the short-circuit off
  and on.
- [x] A deterministic 100-case PHP/Go comparison has zero ordered, physical,
  or box-count differences in both modes.
- [x] Existing short-circuit safety and evaluation-count tests pass.
- [x] `go test ./...` and `go test -race ./...` pass.

## Verification completed

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

## Out of scope

- PHP constrained-placement callbacks.
- Linked-item groups.
- Strict input ordering.
- Timeout checking.
- `packAllPermutations`.
- Replacing the greedy algorithm with a globally optimal solver.
