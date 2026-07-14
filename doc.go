// Package boxpacker solves the 4D bin packing / knapsack problem: fitting
// items into boxes by width, length, depth and weight.
//
// It is a Go port of the dvdoug/boxpacker PHP library
// (https://github.com/dvdoug/boxpacker), using the same layer-based packing
// heuristics, item orientation selection and box selection strategy.
//
// In addition to the PHP algorithm, this implementation offers an opt-in
// short-circuit for packing large quantities. Each box evaluation considers a
// bounded, lookahead-safe number of each item type. With the built-in box
// sorter, solved boxes can also be replicated while the evaluation inputs and
// candidate ordering remain unchanged. Custom sorters retain bounded
// evaluations but disable replication. This keeps large single- and mixed-SKU
// quantities fast without using a separate approximate packing heuristic.
//
// Dimensions are unit-agnostic but must be consistent, and must be integers
// (the reference implementation recommends millimetres and grams). Item and
// Box implementations must be comparable (in practice: use pointer types),
// as identity is used to track items through the packing process.
package boxpacker
