# boxpacker

A Go library for the 4D bin packing / knapsack problem: fitting items into
boxes by **width, length, depth and weight**.

This is a Go port of the excellent [dvdoug/boxpacker](https://github.com/dvdoug/boxpacker)
PHP library, using the same layer-based packing heuristics:

Packing-result compatibility is verified against the
[`feature/quantity-short-circuit`](https://github.com/ryanlholt/DvdBoxPacker/tree/feature/quantity-short-circuit)
branch of the PHP fork for the features shared by both implementations.

- Items are packed largest-first into horizontal layers, in rows, with
  smaller items stacked into the gaps above and beside larger ones.
- All allowed rotations of each item are considered, preferring exact fits,
  orientations that leave room for upcoming items (with a bounded lookahead),
  and stable, low centre-of-gravity placements.
- Box weight limits are enforced during placement, not after.
- Layers are re-ordered bottom-heavy for load stability.
- When multiple box types are available, every type is evaluated through a
  bounded adaptive worker pool, and the box that packs the most items (then
  best volume utilisation) wins. Smaller boxes are preferred when they can
  hold everything, and results are deterministic regardless of goroutine
  scheduling.

## Large quantities

Naively, packing N items costs N/boxCapacity full packing solves —
quadratic-ish behaviour that gets unreasonable at e-commerce quantities. This
port short-circuits that with two cooperating optimisations:

- **Per-type work-bounding.** Each box evaluation is capped to physical
  capacity by volume and weight plus the orientation lookahead window, so the
  size of the order doesn't affect the cost of solving one box — even when the
  order mixes many distinct item types.
- **Box replication.** With the built-in sorter, a solved box's exact item
  makeup can be **replicated** for subsequent boxes while the safety guards
  hold. This works for a winning box made up of a *mix* of item types, not just
  a single type: the multiset of items is what gets replicated.

The optimisation is deliberately conservative. Replication is used only with
the exact built-in box sorter, while every replaced iteration would see the
same bounded item inputs and the same preferred-box candidate partition. The
shrinking tail is solved normally. Custom sorters still benefit from bounded
per-box inputs, but replication is automatically disabled because custom
tie-breaking can depend on candidate order.

The short-circuit is disabled by default. Enable it explicitly for large
orders with `packer.SetQuantityShortCircuit(true)`. The included large-quantity
benchmarks and tests cover both uniform and mixed-SKU workloads and assert a
quantity-independent number of real packing evaluations.

## Weight redistribution

Like the PHP packer, `Pack` performs a post-pack pass by default when the
initial result contains 2 through 12 boxes. It attempts to move items from
heavier boxes to lighter ones when doing so reduces item-weight variance and
both resulting item sets can still be packed into one available box. The pass
never increases box count and respects limited box quantities.

Use `packer.SetMaxBoxesToBalanceWeight(n)` to change the 12-box threshold, or
pass `0` to disable redistribution and keep the initial greedy box makeup.

## Result ordering and utilization

`Pack` returns boxes in the active `PackedBoxSorter` order. The default order
is most items first, then highest volume utilization, then most used volume.
`PackedBox.VolumeUtilisation()` is rounded to one decimal place before that
comparison, matching the PHP feature branch.

Within each box, `Items` is ordered by the original item volume descending,
then item weight descending. Equal-volume, equal-weight items retain their
packing order. These ordering rules also apply when weight redistribution is
disabled or skipped.

## Usage

```go
package main

import (
    "fmt"

    "github.com/ryanlholt/BoxPacker"
)

func main() {
    packer := boxpacker.NewPacker()

    // reference, outer W/L/D, empty weight, inner W/L/D, max weight
    packer.AddBox(boxpacker.NewBox("small mailer", 230, 300, 240, 160, 220, 290, 230, 15000))
    packer.AddBox(boxpacker.NewBox("large mailer", 370, 375, 380, 410, 360, 365, 370, 15000))

    // description, W/L/D, weight, allowed rotation, quantity
    packer.AddItem(boxpacker.NewItem("mug", 110, 110, 105, 350, boxpacker.RotationNever), 4)
    packer.AddItem(boxpacker.NewItem("book", 210, 130, 30, 450, boxpacker.RotationKeepFlat), 2)
    packer.AddItem(boxpacker.NewItem("toy", 80, 60, 60, 150, boxpacker.RotationBestFit), 10)

    packedBoxes, err := packer.Pack()
    if err != nil {
        panic(err) // *boxpacker.NoBoxesAvailableError: an item fits in no box
    }

    for _, box := range packedBoxes {
        fmt.Printf("%s: %d items, %dg total, %.1f%% full\n",
            box.Box.Reference(), len(box.Items), box.Weight(), box.VolumeUtilisation())
        for _, item := range box.Items {
            fmt.Printf("  %s at (%d,%d,%d) packed as %dx%dx%d\n",
                item.Item.Description(), item.X, item.Y, item.Z,
                item.Width, item.Length, item.Depth)
        }
    }
}
```

`Box` and `Item` are interfaces, so you can implement them directly on your
own product/packaging types instead of using `NewBox`/`NewItem`. Use pointer
types: identity is used to track items through packing.

### Options

| Call | Effect |
|------|--------|
| `packer.AllowPartialResults(true)` | Don't error on unpackable items; retrieve leftovers via `packer.UnpackedItems()` |
| `packer.SetQuantityShortCircuit(true)` | Enable lookahead-safe work bounding and, with the built-in sorter, guarded box replication for large quantities |
| `packer.SetMaxConcurrency(n)` | Bound independent box/orientation evaluation: `0` adapts to runtime capacity and workload, `1` is serial, and `n > 1` is a per-packer ceiling |
| `packer.SetMaxBoxesToBalanceWeight(n)` | Rebalance results containing at most `n` boxes by weight; use `0` to disable |
| `packer.AddBox(boxpacker.NewLimitedSupplyBox(...))` / `packer.SetBoxQuantity(box, n)` | Limit how many of a box type are available |
| `packer.SetPackedBoxSorter(sorter)` | Choose which box wins each iteration with a custom objective, e.g. minimising billable shipping weight (see below) |
| `boxpacker.NewVolumePacker(box, items).Pack()` | Pack as much as possible into one specific box |

### Concurrency

Packing is deterministic regardless of the selected concurrency ceiling.
Automatic mode (the default) uses a process-wide worker lease capped by
`GOMAXPROCS`, so simultaneous pack calls cannot collectively exceed the runtime
budget. It stays serial when the input is too small to repay scheduling
overhead. Multiple candidate boxes use one bounded candidate pool; a single
candidate checks its first orientation synchronously and may then evaluate two
remaining orientations concurrently. Recursive lookahead never creates nested
workers.

Use `SetMaxConcurrency(1)` when the caller already owns a worker pool and wants
strictly serial work inside each request. Values greater than one are ceilings,
not promises that the packer will start that many workers. Custom `Box` and
`Item` implementations must permit their getter methods to be called
concurrently and must not be mutated during `Pack`.

### Rotation modes

- `RotationBestFit` — no restrictions, any of the 6 orientations
- `RotationKeepFlat` — may turn sideways 90°, but never on its side ("this way up")
- `RotationNever` — must be packed exactly as dimensioned

### Custom box selection (shipping cost / dim weight)

By default, each packing iteration picks the box that holds the **most items**,
then the **fullest** by volume. That objective minimises parcel count and wasted
space, but it isn't always the cheapest to ship: most carriers bill the greater
of a parcel's actual weight and its **dimensional ("dim") weight**
(`outerVolume / divisor`), so a large, lightly-filled box can cost more than two
compact ones.

`SetPackedBoxSorter` replaces the box-selection objective without touching the
packing geometry. Implement `PackedBoxSorter` (or pass a `PackedBoxSorterFunc`),
returning `< 0` if `a` is the better box, `> 0` if `b` is, `0` if equal. The
`BillableWeight` helper computes `max(actualGrossWeight, volumetricWeight)` for a
packed box, and `VolumetricWeight` computes the dim-weight component alone:

```go
const divisor = 5000 // e.g. cm dimensions billed in kg; use 139 for inches->lb

packer.SetPackedBoxSorter(boxpacker.PackedBoxSorterFunc(func(a, b *boxpacker.PackedBox) int {
    aw, bw := boxpacker.BillableWeight(a, divisor), boxpacker.BillableWeight(b, divisor)
    switch {
    case aw < bw:
        return -1
    case aw > bw:
        return 1
    default:
        return 0 // fall back to the default order, or add your own tie-break
    }
}))
```

Dimensions and the divisor must be in consistent units (and match your item
weight unit); no carrier-specific rounding is applied, so layer that on top if
you need it. Passing `nil` restores the default ordering.

Two caveats:

- The solver is still **greedy** — this changes the per-parcel choice, not the
  global cost across every box, so it won't reason about carrier rate-tier
  thresholds spanning multiple parcels.
- When the quantity short-circuit is enabled with a custom sorter, safe per-box
  item capping remains active but solved-box replication is automatically
  disabled. No additional option is required for custom-sorter parity.

### Units

Dimensions and weights are integers and unit-agnostic — just be consistent.
Millimetres and grams are recommended (matching the PHP library); avoid
centimetres/inches if you need sub-unit precision, since there are no
fractions.

## Differences from the PHP library

- Adds the opt-in large-quantity short-circuit described above.
- Includes PHP-compatible post-packing weight redistribution for results of up
  to 12 boxes by default.
- Supports a custom `PackedBoxSorter` (like the PHP library), plus
  `BillableWeight`/`VolumetricWeight` helpers for dim-weight-aware objectives.
- No `ConstrainedPlacementItem` callbacks, linked-item groups, strict input
  ordering, timeout checker, or `packAllPermutations`.
- Errors are returned as values (`*NoBoxesAvailableError`) rather than thrown.

A `Packer` is single-use and not safe for concurrent use; create one per
packing operation. Separate `Packer` instances may be used concurrently.
