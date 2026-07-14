# S-1: Restore the PHP core orientation heuristic

Status: Complete

## Goal

Make `VolumePacker` choose the first item's orientation through the same
bounded-lookahead sorter used by PHP instead of exhaustively trying every
first-item orientation and selecting the best completed pack.

## Scope

- Remove the Go-only first-item-orientation permutation loop.
- Remove the special forced-first-orientation path from layer packing.
- Keep both supported box-width permutations and the existing
  `RotationNever` protection.
- Add a regression fixture whose PHP result is two boxes but whose current Go
  result is one box because of the exhaustive search.

## Acceptance criteria

- [x] The first item in each layer is selected by `getBestOrientation`, just
  like every following item.
- [x] The parity regression packs nine `SKU0` items and four `SKU1` items into
  the same two box types and SKU distribution as the PHP fork.
- [x] Every produced placement remains in bounds, non-overlapping, and within
  the box weight limit.
- [x] Existing rotation-mode tests continue to pass.
- [x] `go test ./...` passes.

## Verification completed

- `go test ./...`
- `go test -race ./...`
- Targeted parity and rotation tests
- 100-case PHP-fork comparison: 100 exact canonical physical-packing matches

## Verification fixture

- Small box: `107 x 118 x 125`
- Large box: `170 x 160 x 150`
- `SKU0`: `45 x 92 x 54`, weight `322`, quantity `9`, best-fit rotation
- `SKU1`: `55 x 70 x 41`, weight `376`, quantity `4`, keep-flat rotation
- PHP result: one large box containing all nine `SKU0` plus two `SKU1`, and one
  small box containing the remaining two `SKU1`.

## Out of scope

- Quantity short-circuit correctness.
- Weight redistribution and result ordering.
- An optional enhanced/exhaustive packing mode; it can be reconsidered after
  the compatibility path is stable.
