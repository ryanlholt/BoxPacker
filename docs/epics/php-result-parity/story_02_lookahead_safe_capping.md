# S-2: Make per-box quantity capping lookahead-safe

## Goal

Bound large quantities without changing the up-to-eight-item lookahead window
used to select orientations.

## Scope

- Introduce a named lookahead-depth constant shared by capping assumptions and
  orientation lookahead.
- Cap each signature at physical capacity plus lookahead depth.
- Return the original list unchanged when no signature needs capping.
- Port the PHP fork's capacity-below-lookahead regression.

## Acceptance criteria

- [ ] Capped and uncapped evaluation produce identical canonical placements
  when physical capacity is below the lookahead depth.
- [ ] Capping work remains independent of total quantity.
- [ ] Zero-volume, zero-weight, and overweight capacity calculations remain
  safe.
- [ ] `go test ./...` passes.

## Dependencies

- S-1
