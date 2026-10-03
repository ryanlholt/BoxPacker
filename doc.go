// Package boxpacker solves the 4D bin packing / knapsack problem: fitting
// items into boxes by width, length, depth and weight.
//
// It is a Go port of the dvdoug/boxpacker PHP library
// (https://github.com/dvdoug/boxpacker), using the same layer-based packing
// heuristics, item orientation selection and box selection strategy.
//
// In addition to the PHP algorithm, this implementation offers an opt-in
// short-circuit for packing large quantities. Repeated input identities use
// compact quantity entries; returned placements remain independently owned.
// Each box evaluation considers a
// bounded, lookahead-safe number of each item type. With the built-in box
// sorter, solved boxes can also be replicated while the evaluation inputs and
// candidate ordering remain unchanged. Custom sorters retain bounded
// evaluations but disable replication. This keeps large single- and mixed-SKU
// quantities fast without using a separate approximate packing heuristic.
// Optional Packer.SetPackingSearchBudget enables bounded improvement search
// for complete orders of at most 16 items with the default sorter. It can reduce
// box count or outer volume, but does not guarantee global optimality.
// Results containing two through twelve boxes are rebalanced by item weight by
// default, matching the PHP packer's post-pack redistribution behavior; the
// threshold is configurable and can be set to zero to disable redistribution.
// Independent candidate-box and first-item-orientation evaluations use an
// adaptive bounded scheduler. It respects GOMAXPROCS, shares capacity across
// simultaneous pack calls, preserves indexed tie-breaking, and can be forced
// serial with Packer.SetMaxConcurrency(1).
// Returned boxes follow the active PackedBoxSorter, packed items are ordered by
// original volume and weight, and volume utilization is rounded to one decimal
// place, matching the observable PHP 4.x result semantics for shared features.
//
// Dimensions are unit-agnostic but must be consistent, and must be integers
// (the reference implementation recommends millimetres and grams). Item and
// Box implementations must be comparable (in practice: use pointer types), as
// identity is used to track items through the packing process. Their getter
// methods must also be safe for concurrent reads.
package boxpacker
