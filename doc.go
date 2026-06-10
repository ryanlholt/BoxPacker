// Package boxpacker solves the 4D bin packing / knapsack problem: fitting
// items into boxes by width, length, depth and weight.
//
// It is a Go port of the dvdoug/boxpacker PHP library
// (https://github.com/dvdoug/boxpacker), using the same layer-based packing
// heuristics, item orientation selection and box selection strategy.
//
// In addition to the PHP algorithm, this implementation short-circuits
// packing when large quantities of identical items are being packed: once a
// box has been packed full of a single item type and only more of that same
// item type remains, the same packing configuration is replicated for
// subsequent boxes rather than re-solved from scratch. This keeps packing of
// very large quantities (tens or hundreds of thousands of units) fast.
//
// Dimensions are unit-agnostic but must be consistent, and must be integers
// (the reference implementation recommends millimetres and grams). Item and
// Box implementations must be comparable (in practice: use pointer types),
// as identity is used to track items through the packing process.
package boxpacker
