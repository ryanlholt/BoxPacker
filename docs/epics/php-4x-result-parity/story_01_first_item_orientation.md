# S-1: Port the PHP 4.x first-item orientation search

Status: Complete

## Goal

Match PHP 4.x by evaluating every valid orientation of the first item in a box
and selecting the completed packing with the highest volume utilization.

## Scope

- Generate every valid first-item orientation for each supported box-width
  permutation outside single-pass lookahead mode.
- Pass the selected orientation into the first layer placement.
- Return to normal bounded-lookahead orientation selection after the first
  item has been placed.
- Preserve PHP's early return when one orientation fits every remaining item.
- Exercise the behavior with the quantity short-circuit off and on.

## Acceptance criteria

- [x] The nine `SKU0` plus four `SKU1` fixture packs into one large box in both
  short-circuit modes.
- [x] Produced placements remain in bounds, non-overlapping, and within the
  box weight limit.
- [x] The feature-branch golden corpus matches in both modes.
- [x] The seeded 100-case cross-language comparison matches in both modes.
- [x] Full, race, and performance verification passes.

## Verification completed

- Exact feature-branch golden corpus in both short-circuit modes.
- 100/100 exact ordered and canonical matches with the short-circuit disabled.
- 100/100 exact ordered and canonical matches with the short-circuit enabled.
- PHP 250-case optimization fuzz suite: zero divergences.
- `go test ./...`
- `go test -race ./...`

## Compatibility note

This is an intentional packing-quality enhancement over PHP 3.x and can change
box makeup, coordinates, orientations, and box count. The prior two-box result
remains pinned by the `3.x-parity` branch rather than by this branch.
