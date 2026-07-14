# S-3: Guard box replication across a depleting item pool

Status: Complete

## Goal

Replicate a solved box only while every replaced packing iteration would see
the same bounded item pool and therefore make the same placement decisions.

## Scope

- Compute the maximum capacity for each constituent signature across all
  in-stock candidate boxes.
- Require `max capacity + lookahead depth` stock at every replicated
  iteration.
- Leave the shrinking tail to normal packing.
- Preserve limited-supply box accounting and multiset removal.
- Port the PHP fork's depleted-pool orientation regression.

## Acceptance criteria

- [x] Short-circuit on and off produce identical canonical boxes for the
  depleted-pool regression.
- [x] Limited box supply cannot be exceeded by replication.
- [x] Large uniform and mixed quantities still require a quantity-independent
  number of real packing evaluations.
- [x] `go test ./...` passes.

## Verification completed

- Ported the PHP fork's 11-item depleted-pool orientation fixture and compared
  canonical physical placements with the short-circuit on and off.
- Verified that replication consumes exactly three available boxes from a
  limited supply and leaves the remaining 27 items unpacked.
- Instrumented real candidate-box evaluations; quantities of 100 and 10,000
  use the same bounded count for a uniform SKU, and quantities of 100 and 1,000
  use the same bounded count for a repeating mixed-SKU box.
- Seeded 500-case short-circuit differential: zero canonical packing
  differences and zero box-count differences.
- `go test ./...`
- `go test -race ./...`

## Dependencies

- S-2
