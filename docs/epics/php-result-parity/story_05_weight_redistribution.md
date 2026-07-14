# S-5: Port post-pack weight redistribution

Status: Complete

## Goal

Match PHP's default behavior of redistributing items across small multi-box
results to reduce weight variance without increasing the number of boxes.

## Scope

- Port the 3.x `WeightRedistributor` behavior.
- Apply it by default to results containing 2 through 12 boxes.
- Add a configurable maximum matching PHP's `maxBoxesToBalanceWeight`.
- Preserve box quantities and the active packed-box sorter.

## Acceptance criteria

- [x] Known PHP redistribution fixtures match box makeup and placement.
- [x] Redistribution never increases box count or violates box supply.
- [x] Callers can set the maximum number of boxes to rebalance, including zero
  to disable it.
- [x] `go test ./...` passes.

## Verification completed

- Ported the PHP 3.x two-box `3+1` to `2+2` fixture with exact canonical
  coordinates and orientations.
- Ported the mixed keep-flat fixture; both output boxes contain one `Item 1`
  and two `Item 2` instances at the same placements as PHP, with zero weight
  variance.
- Pinned PHP's cumulative tentative-transfer behavior with a multi-SKU fixture
  that exercises a temporarily unsuccessful heavier-side repack.
- Verified the default maximum is 12, zero disables redistribution, and raising
  the limit from 12 to 13 activates redistribution for a 13-box result.
- Verified limited box quantities remain accurate after replacement repacks and
  the active packed-box sorter controls the returned ordering.
- 100-case PHP/Go balanced-packing corpus: 100 exact canonical physical-packing
  matches, including box makeup, coordinates, and orientations.
- Seeded 500-case short-circuit differential after redistribution: zero
  canonical packing differences and zero box-count differences.
- `go test ./...`
- `go test -race ./...`

## Dependencies

- S-4
