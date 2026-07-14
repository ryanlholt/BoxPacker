# S-3: Guard box replication across a depleting item pool

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

- [ ] Short-circuit on and off produce identical canonical boxes for the
  depleted-pool regression.
- [ ] Limited box supply cannot be exceeded by replication.
- [ ] Large uniform and mixed quantities still require a quantity-independent
  number of real packing evaluations.
- [ ] `go test ./...` passes.

## Dependencies

- S-2
