# S-2: Make per-box quantity capping lookahead-safe

Status: Complete

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

- [x] Capped and uncapped evaluation produce identical canonical placements
  when physical capacity is below the lookahead depth.
- [x] Capping work remains independent of total quantity.
- [x] Zero-volume, zero-weight, and overweight capacity calculations remain
  safe.
- [x] `go test ./...` passes.

## Verification completed

- The PHP fork's capacity-below-lookahead fixture failed before the change and
  passed after adding physical capacity plus eight items of headroom.
- Direct bounded-list tests cover quantities of 100 and 100,000 with the same
  evaluation size.
- Direct capacity tests cover zero-volume, zero-weight, and overweight items.
- `go test ./...`
- `go test -race ./...`

## Dependencies

- S-1
