# S-5: Port post-pack weight redistribution

## Goal

Match PHP's default behavior of redistributing items across small multi-box
results to reduce weight variance without increasing the number of boxes.

## Scope

- Port the 3.x `WeightRedistributor` behavior.
- Apply it by default to results containing 2 through 12 boxes.
- Add a configurable maximum matching PHP's `maxBoxesToBalanceWeight`.
- Preserve box quantities and the active packed-box sorter.

## Acceptance criteria

- [ ] Known PHP redistribution fixtures match box makeup and placement.
- [ ] Redistribution never increases box count or violates box supply.
- [ ] Callers can set the maximum number of boxes to rebalance, including zero
  to disable it.
- [ ] `go test ./...` passes.

## Dependencies

- S-4
