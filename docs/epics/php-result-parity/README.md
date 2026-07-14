# PHP result parity

## Objective

Make the Go packer reproduce the packing decisions of the
`dvdoug/boxpacker` 3.x algorithm and the
`ryanlholt/DvdBoxPacker:3.x-quantity-short-circuit` fork for features shared by
both implementations, while retaining a quantity optimisation that is proven
not to change those decisions.

Parity means that the same boxes contain the same item signatures at the same
coordinates and orientations. Collection ordering is treated separately in
Story 6 because it does not change the physical packing.

## Why this epic exists

Evaluation found two independent sources of drift:

- The Go-only exhaustive search over every orientation of the first item
  changed 64 of 100 sampled physical packings and changed the box count in 2
  cases. Removing only that search produced 100 of 100 matches with the PHP
  fork when both quantity optimisation and PHP weight redistribution were
  disabled.
- The Go quantity short-circuit is not result-preserving. A 500-case seeded
  differential run found 116 physical-packing differences between the
  optimisation being off and on, including 17 different box counts.

The PHP default weight-redistribution pass changed 30 of the 100 otherwise
matching sampled packings, making that the next substantial parity gap after
the core heuristic and quantity optimisation.

## Scope

- Restore the PHP first-item orientation decision path.
- Preserve the orientation lookahead window when bounding large quantities.
- Replicate solved boxes only while subsequent packing inputs and candidate
  ordering are provably unchanged.
- Make the quantity optimisation opt-in until equivalence is established.
- Port the PHP weight-redistribution phase for small multi-box results.
- Match result-affecting rounding and observable collection ordering.
- Add deterministic, placement-aware differential tests.

## Out of scope

- PHP constrained-placement callbacks and deprecated constrained-item hooks.
- Strict input ordering, timeout checking, `packAllPermutations`, and the PHP
  `InfalliblePacker` API.
- Removing the Go-only `RotationNever` mode or carrier billable-weight helpers.
- Replacing the greedy algorithm with a globally optimal bin-packing solver.

## Story order

1. [S-1: Restore the PHP core orientation heuristic](story_01_core_orientation_parity.md)
2. [S-2: Make per-box quantity capping lookahead-safe](story_02_lookahead_safe_capping.md)
3. [S-3: Guard box replication across a depleting item pool](story_03_safe_replication.md)
4. [S-4: Preserve sorter and candidate-order semantics](story_04_sorter_and_candidate_order.md)
5. [S-5: Port post-pack weight redistribution](story_05_weight_redistribution.md)
6. [S-6: Align result semantics and establish the parity suite](story_06_result_semantics_and_parity_suite.md)

Stories are ordered by dependency. S-2 through S-4 together establish the
short-circuit equivalence claim; the claim must not be restored in public
documentation until all three are complete.

## Epic acceptance criteria

- [ ] The shared-feature cross-language corpus produces identical canonical
  physical packings.
- [ ] Quantity short-circuit on and off produce identical canonical physical
  packings across hand-picked regressions and a seeded differential suite.
- [ ] Large single- and mixed-SKU quantities remain quantity-independent in
  the number of real box evaluations performed.
- [ ] Default PHP weight redistribution, utilization rounding, and result
  ordering are represented or explicitly configurable in Go.
- [ ] The README describes guarantees and differences that are true of the
  verified implementation.

## Verification approach

- Canonicalize each packed box as its box reference plus sorted item entries
  containing description, XYZ position, and packed width/length/depth.
- Compare sorted canonical boxes so physical parity is not confused with API
  ordering.
- Maintain explicit fixtures for every known divergence.
- Use evaluation counts for performance assertions rather than wall-clock
  thresholds where possible.
- Run `go test ./...` after every story.
