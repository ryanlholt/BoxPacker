// Package boxpacker solves the 4D bin packing / knapsack problem: fitting
// items into boxes by width, length, depth and weight.
//
// It is a Go port of the dvdoug/boxpacker PHP library
// (https://github.com/dvdoug/boxpacker), using the same layer-based packing
// heuristics, item orientation selection and box selection strategy.
//
// In addition to the PHP algorithm, this implementation short-circuits
// packing of large quantities. Each box evaluation only considers as many of
// each item type as that box could physically hold, and once a box has been
// solved its exact item makeup - whether a single item type or a mix of
// several - is replicated for as long as the pool can supply more identical
// boxfuls, rather than re-solved from scratch. This keeps packing of very
// large quantities (tens or hundreds of thousands of units), across one or
// many distinct item types, fast.
//
// Dimensions are unit-agnostic but must be consistent, and must be integers
// (the reference implementation recommends millimetres and grams). Item and
// Box implementations must be comparable (in practice: use pointer types),
// as identity is used to track items through the packing process.
package boxpacker
