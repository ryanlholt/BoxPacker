# S-4: Preserve sorter and candidate-order semantics

Status: Complete

## Goal

Prevent replication from skipping decisions that can change because of a
custom sorter or a changing preferred-box partition.

## Scope

- Permit replication only with the exact built-in packed-box sorter.
- Keep safe per-box capping enabled for custom sorters.
- Stop replication before remaining volume changes candidate-box partition
  order.
- Make quantity short-circuit opt-in by default.
- Correct README guarantees and option examples.

## Acceptance criteria

- [x] A no-preference custom sorter yields the same boxes with short-circuit on
  and off.
- [x] Default-sorter replication stops at the preferred-box boundary.
- [x] `NewPacker` does not enable quantity short-circuit implicitly.
- [x] Documentation makes no unverified exactness claim.
- [x] `go test ./...` passes.

## Verification completed

- Ported the PHP fork's no-preference custom-sorter fixture: short-circuit off
  and on both produce 12 `Small` boxes and one `Large` box, while all 13 boxes
  are solved by real packing iterations. Per-box capping remains active.
- Ported the two-box preferred-partition fixture and verified replication stops
  at the boundary, yielding seven real packing iterations.
- Added an equality-boundary regression so an iteration is not replicated when
  remaining item volume exactly equals a previously non-preferred box volume.
- Added a zero-volume-template regression to ensure the volume guard does not
  introduce an artificial replication limit or division by zero.
- Confirmed `NewPacker` leaves the optimisation disabled until explicitly
  enabled, and updated package and README documentation accordingly.
- Seeded 500-case short-circuit differential: zero canonical packing
  differences and zero box-count differences.
- `go test ./...`
- `go test -race ./...`

## Dependencies

- S-3
