package boxpacker

import (
	"sort"
	"strings"
)

// Item is an item to be packed.
//
// Implementations must be comparable with == (use a pointer type), as item
// identity is used to track items through the packing process. Methods may be
// called concurrently during packing and must be safe for concurrent reads.
type Item interface {
	// Description returns the item SKU, name etc.
	Description() string

	// Width of the item.
	Width() int

	// Length of the item.
	Length() int

	// Depth (height) of the item.
	Depth() int

	// Weight of the item.
	Weight() int

	// AllowedRotation returns the rotations the item may be placed in.
	AllowedRotation() Rotation
}

// StandardItem is a simple immutable Item implementation.
type StandardItem struct {
	description                  string
	width, length, depth, weight int
	rotation                     Rotation
}

// NewItem creates a StandardItem.
func NewItem(description string, width, length, depth, weight int, rotation Rotation) *StandardItem {
	return &StandardItem{
		description: description,
		width:       width,
		length:      length,
		depth:       depth,
		weight:      weight,
		rotation:    rotation,
	}
}

func (i *StandardItem) Description() string       { return i.description }
func (i *StandardItem) Width() int                { return i.width }
func (i *StandardItem) Length() int               { return i.length }
func (i *StandardItem) Depth() int                { return i.depth }
func (i *StandardItem) Weight() int               { return i.weight }
func (i *StandardItem) AllowedRotation() Rotation { return i.rotation }

func itemVolume(i Item) int {
	return i.Width() * i.Length() * i.Depth()
}

// compareItems orders by volume (descending), then weight (descending), then
// description (ascending), matching the PHP DefaultItemSorter.
func compareItems(a, b Item) int {
	av, bv := itemVolume(a), itemVolume(b)
	if av != bv {
		if av > bv {
			return -1
		}
		return 1
	}
	if a.Weight() != b.Weight() {
		if a.Weight() > b.Weight() {
			return -1
		}
		return 1
	}
	return strings.Compare(a.Description(), b.Description())
}

// itemSignature identifies physically-interchangeable items.
type itemSignature struct {
	description                  string
	width, length, depth, weight int
	rotation                     Rotation
}

func signatureOf(item Item) itemSignature {
	return itemSignature{
		description: item.Description(),
		width:       item.Width(),
		length:      item.Length(),
		depth:       item.Depth(),
		weight:      item.Weight(),
		rotation:    item.AllowedRotation(),
	}
}

// hasSortTiedSignature reports physically different signatures that tie in
// compareItems. Their interleaved stable order prevents count-only replication.
func hasSortTiedSignature(target itemSignature, counts map[itemSignature]int) bool {
	targetVolume := target.width * target.length * target.depth
	for candidate := range counts {
		if candidate == target {
			continue
		}
		if candidate.width*candidate.length*candidate.depth == targetVolume &&
			candidate.weight == target.weight &&
			candidate.description == target.description {
			return true
		}
	}
	return false
}

// itemRun retains identity and insertion order while storing repeated copies
// without allocating one interface value per copy.
type itemRun struct {
	item     Item
	quantity int
}

// itemList is ordered largest-first unless an internal caller supplies an
// already ordered list. Clones own their runs; Item values remain immutable.
type itemList struct {
	runs     []itemRun
	size     int
	isSorted bool
}

func newItemListFromSlice(items []Item, preSorted bool) *itemList {
	l := &itemList{}
	for _, item := range items {
		l.insert(item, 1)
	}
	l.isSorted = preSorted
	return l
}

func (l *itemList) insert(item Item, qty int) {
	if qty <= 0 {
		return
	}
	l.appendRun(itemRun{item, qty})
	l.isSorted = false
}

func (l *itemList) appendRun(run itemRun) {
	if run.quantity <= 0 {
		return
	}
	if n := len(l.runs); n > 0 && l.runs[n-1].item == run.item {
		l.runs[n-1].quantity += run.quantity
	} else {
		l.runs = append(l.runs, run)
	}
	l.size += run.quantity
}

func (l *itemList) ensureSorted() {
	if l.isSorted {
		return
	}
	sort.SliceStable(l.runs, func(i, j int) bool { return compareItems(l.runs[i].item, l.runs[j].item) < 0 })
	l.isSorted = true
}

func (l *itemList) count() int { return l.size }

func (l *itemList) clone() *itemList {
	return &itemList{runs: append([]itemRun(nil), l.runs...), size: l.size, isSorted: l.isSorted}
}

func (l *itemList) extract() Item {
	item := l.top()
	l.removeFirstN(1)
	return item
}

func (l *itemList) top() Item {
	l.ensureSorted()
	return l.runs[0].item
}

func (l *itemList) topN(n int) *itemList {
	l.ensureSorted()
	out := &itemList{isSorted: true}
	for _, run := range l.runs {
		if n <= 0 {
			break
		}
		run.quantity = minInt(run.quantity, n)
		out.appendRun(run)
		n -= run.quantity
	}
	return out
}

// toSlice expands quantities only at an API boundary or for bounded lookahead.
func (l *itemList) toSlice() []Item {
	l.ensureSorted()
	out := make([]Item, 0, l.size)
	for _, run := range l.runs {
		for range run.quantity {
			out = append(out, run.item)
		}
	}
	return out
}

// restore consumes a skipped list without expanding its quantities. Callers
// must have exhausted the current list, preserving the original sorted order.
func (l *itemList) restore(skipped *itemList) {
	*l = *skipped
	l.isSorted = true
	*skipped = itemList{}
}

// removePackedItems removes exact identities in one stable pass.
func (l *itemList) removePackedItems(packed []*PackedItem) {
	counts := make(map[Item]int, len(packed))
	for _, pi := range packed {
		counts[pi.Item]++
	}
	l.ensureSorted()
	out := l.runs[:0]
	for _, run := range l.runs {
		removed := minInt(run.quantity, counts[run.item])
		counts[run.item] -= removed
		run.quantity -= removed
		l.size -= removed
		if run.quantity > 0 {
			out = append(out, run)
		}
	}
	clear(l.runs[len(out):])
	l.runs = out
}

func (l *itemList) removeFirstN(n int) {
	l.ensureSorted()
	for n > 0 {
		removed := minInt(n, l.runs[0].quantity)
		l.runs[0].quantity -= removed
		l.size -= removed
		n -= removed
		if l.runs[0].quantity == 0 {
			l.runs[0] = itemRun{}
			l.runs = l.runs[1:]
		}
	}
}

func (l *itemList) hasNoRotationItems() bool {
	for _, run := range l.runs {
		if run.item.AllowedRotation() == RotationNever {
			return true
		}
	}
	return false
}

func (l *itemList) totalVolume() int {
	volume := 0
	for _, run := range l.runs {
		volume += itemVolume(run.item) * run.quantity
	}
	return volume
}

func (l *itemList) signatureCounts() map[itemSignature]int {
	counts := make(map[itemSignature]int)
	for _, run := range l.runs {
		counts[signatureOf(run.item)] += run.quantity
	}
	return counts
}

func (l *itemList) cappedBySignature(caps map[itemSignature]int) *itemList {
	l.ensureSorted()
	out := &itemList{isSorted: true}
	used := make(map[itemSignature]int, len(caps))
	for _, run := range l.runs {
		sig := signatureOf(run.item)
		run.quantity = minInt(run.quantity, caps[sig]-used[sig])
		if run.quantity > 0 {
			used[sig] += run.quantity
			out.appendRun(run)
		}
	}
	return out
}

// takeSignatureMultiset consumes matching identities in stable order. The
// returned compact queues let replicas bind their layout to actual input items.
func (l *itemList) takeSignatureMultiset(toRemove map[itemSignature]int) map[itemSignature]*itemList {
	l.ensureSorted()
	taken := make(map[itemSignature]*itemList, len(toRemove))
	remaining := make(map[itemSignature]int, len(toRemove))
	for sig, n := range toRemove {
		remaining[sig] = n
	}
	out := l.runs[:0]
	for _, run := range l.runs {
		sig := signatureOf(run.item)
		removed := minInt(run.quantity, remaining[sig])
		if removed > 0 {
			if taken[sig] == nil {
				taken[sig] = &itemList{isSorted: true}
			}
			taken[sig].appendRun(itemRun{run.item, removed})
			remaining[sig] -= removed
			run.quantity -= removed
			l.size -= removed
		}
		if run.quantity > 0 {
			out = append(out, run)
		}
	}
	clear(l.runs[len(out):])
	l.runs = out
	return taken
}

// samePackingDimensions permits skipping only items with the same set of
// allowed orientations, including the original axis order for fixed items.
func samePackingDimensions(a, b Item) bool {
	if a == b {
		return true
	}
	if a.AllowedRotation() != b.AllowedRotation() {
		return false
	}
	switch a.AllowedRotation() {
	case RotationBestFit:
		return isSameDimensions(a, b)
	case RotationKeepFlat:
		return a.Depth() == b.Depth() && sortedDims(a.Width(), a.Length(), 0) == sortedDims(b.Width(), b.Length(), 0)
	default:
		return a.Width() == b.Width() && a.Length() == b.Length() && a.Depth() == b.Depth()
	}
}

// isSameDimensions reports whether two items have the same dimensions in any
// orientation.
func isSameDimensions(a, b Item) bool {
	if a == b {
		return true
	}
	aDims := sortedDims(a.Width(), a.Length(), a.Depth())
	bDims := sortedDims(b.Width(), b.Length(), b.Depth())
	return aDims == bDims
}

func sortedDims(a, b, c int) [3]int {
	if a > b {
		a, b = b, a
	}
	if b > c {
		b, c = c, b
	}
	if a > b {
		a, b = b, a
	}
	return [3]int{a, b, c}
}
