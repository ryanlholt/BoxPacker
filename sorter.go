package boxpacker

// PackedBoxSorter decides which of two candidate packed boxes is the better
// choice. Each packing iteration evaluates every box type and then asks the
// sorter to pick a winner; the winner's items are committed and the process
// repeats. Supplying a custom sorter lets callers optimise for an objective
// other than the default (most items, then fullest), e.g. minimising billable
// shipping weight or looking up a per-box rate table.
//
// Compare reports whether a should be preferred over b:
//
//	< 0  a is the better box (sorts earlier)
//	  0  the two are equally good
//	> 0  b is the better box
//
// Compare must impose a deterministic total order and must not mutate either
// box.
type PackedBoxSorter interface {
	Compare(a, b *PackedBox) int
}

// PackedBoxSorterFunc adapts an ordinary func to a PackedBoxSorter, so callers
// can pass a comparison function without declaring a type.
type PackedBoxSorterFunc func(a, b *PackedBox) int

// Compare implements PackedBoxSorter.
func (f PackedBoxSorterFunc) Compare(a, b *PackedBox) int { return f(a, b) }

// defaultPackedBoxSorter reproduces the library's built-in ordering: most items
// first, then highest volume utilisation, then most used volume. It matches the
// PHP DefaultPackedBoxSorter and is used when no custom sorter is set.
type defaultPackedBoxSorter struct{}

// Compare implements PackedBoxSorter.
func (defaultPackedBoxSorter) Compare(a, b *PackedBox) int { return comparePackedBoxes(a, b) }
