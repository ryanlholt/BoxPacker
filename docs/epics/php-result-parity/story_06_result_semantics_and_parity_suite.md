# S-6: Align result semantics and establish the parity suite

Status: Complete

## Goal

Close remaining shared-feature differences and make cross-language drift a
permanent test failure.

## Scope

- Match PHP's one-decimal volume-utilization rounding where it affects
  candidate ties.
- Define and match returned packed-box and packed-item ordering.
- Add canonical physical-packing helpers for tests.
- Check in a deterministic cross-language golden corpus covering best-fit and
  keep-flat items, weight limits, multiple box sizes, and limited supply.
- Add seeded short-circuit-on/off differential coverage.

## Acceptance criteria

- [x] The checked-in PHP corpus matches Go canonical packings.
- [x] Public result ordering and utilization values match the documented PHP
  compatibility contract.
- [x] Seeded short-circuit differential tests compare complete placements, not
  just box and item counts.
- [x] Performance tests verify bounded evaluation counts.
- [x] Epic README acceptance criteria are checked only after full verification.
- [x] `go test ./...` passes.

## Verification completed

- `PackedBox.VolumeUtilisation` now matches PHP's one-decimal rounding and the
  rounded candidate-tie fixture selects 14 `BoxA` instances and one `BoxB`.
- Returned boxes always follow the active `PackedBoxSorter`, including when
  redistribution is disabled or skipped. Returned items follow PHP's original
  volume-descending, weight-descending order.
- Checked in `testdata/php_parity_corpus.json`, generated from PHP fork commit
  `ec5663dac4a7630368296ab87800db48a2e2d67a`. Its ordered expectations cover
  best-fit and keep-flat rotation, weight limits, balancing, multiple box sizes,
  and limited supply.
- The checked-in 100-case seeded differential compares canonical descriptions,
  coordinates, and packed dimensions with the short-circuit off and on.
- The existing real-evaluation observer verifies quantity-independent bounds
  for uniform quantities of 100 and 10,000 and mixed quantities of 100 and
  1,000.
- External 100-case PHP/Go balanced corpus: 100 exact canonical matches.
- Extended 500-case short-circuit differential: zero canonical differences and
  zero box-count differences.
- `go test ./...`
- `go test -race ./...`

## Dependencies

- S-5
