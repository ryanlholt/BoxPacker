# S-6: Align result semantics and establish the parity suite

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

- [ ] The checked-in PHP corpus matches Go canonical packings.
- [ ] Public result ordering and utilization values match the documented PHP
  compatibility contract.
- [ ] Seeded short-circuit differential tests compare complete placements, not
  just box and item counts.
- [ ] Performance tests verify bounded evaluation counts.
- [ ] Epic README acceptance criteria are checked only after full verification.
- [ ] `go test ./...` passes.

## Dependencies

- S-5
