# S-4: Preserve sorter and candidate-order semantics

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

- [ ] A no-preference custom sorter yields the same boxes with short-circuit on
  and off.
- [ ] Default-sorter replication stops at the preferred-box boundary.
- [ ] `NewPacker` does not enable quantity short-circuit implicitly.
- [ ] Documentation makes no unverified exactness claim.
- [ ] `go test ./...` passes.

## Dependencies

- S-3
