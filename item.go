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

// hasSortTiedSignature reports whether a physically different signature is
// indistinguishable from target to compareItems. Stable sorting may interleave
// such items, so seeing target at the head does not prove a whole prefix has
// that signature.
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

// itemList is a list of items to be packed, ordered largest-first.
type itemList struct {
	list     []Item
	isSorted bool
}

func newItemListFromSlice(items []Item, preSorted bool) *itemList {
	l := &itemList{list: make([]Item, len(items)), isSorted: preSorted}
	copy(l.list, items)
	return l
}

func (l *itemList) insert(item Item, qty int) {
	for i := 0; i < qty; i++ {
		l.list = append(l.list, item)
	}
	l.isSorted = false
}

func (l *itemList) ensureSorted() {
	if l.isSorted {
		return
	}
	sort.SliceStable(l.list, func(i, j int) bool { return compareItems(l.list[i], l.list[j]) < 0 })
	l.isSorted = true
}

func (l *itemList) count() int {
	return len(l.list)
}

func (l *itemList) clone() *itemList {
	c := &itemList{list: make([]Item, len(l.list)), isSorted: l.isSorted}
	copy(c.list, l.list)
	return c
}

// extract removes and returns the largest remaining item.
func (l *itemList) extract() Item {
	l.ensureSorted()
	item := l.list[0]
	l.list = l.list[1:]
	return item
}

// top returns the largest remaining item without removing it.
func (l *itemList) top() Item {
	l.ensureSorted()
	return l.list[0]
}

// topN returns a new list containing the n largest remaining items.
func (l *itemList) topN(n int) *itemList {
	l.ensureSorted()
	if n > len(l.list) {
		n = len(l.list)
	}
	return newItemListFromSlice(l.list[:n], true)
}

// toSlice returns a sorted copy of the remaining items.
func (l *itemList) toSlice() []Item {
	l.ensureSorted()
	out := make([]Item, len(l.list))
	copy(out, l.list)
	return out
}

// replace swaps the list contents for the given items.
func (l *itemList) replace(items []Item, preSorted bool) {
	l.list = make([]Item, len(items))
	copy(l.list, items)
	l.isSorted = preSorted
}

// removePackedItems removes the given packed items (by identity) from the list.
func (l *itemList) removePackedItems(packed []*PackedItem) {
	l.ensureSorted()
	for _, pi := range packed {
		for i, item := range l.list {
			if item == pi.Item {
				l.list = append(l.list[:i], l.list[i+1:]...)
				break
			}
		}
	}
}

// removeFirstN drops the n largest remaining items.
func (l *itemList) removeFirstN(n int) {
	l.ensureSorted()
	l.list = l.list[n:]
}

func (l *itemList) hasNoRotationItems() bool {
	for _, item := range l.list {
		if item.AllowedRotation() == RotationNever {
			return true
		}
	}
	return false
}

func (l *itemList) totalVolume() int {
	volume := 0
	for _, item := range l.list {
		volume += itemVolume(item)
	}
	return volume
}

// signatureCounts returns how many items of each distinct signature the list
// holds.
func (l *itemList) signatureCounts() map[itemSignature]int {
	counts := make(map[itemSignature]int)
	for _, item := range l.list {
		counts[signatureOf(item)]++
	}
	return counts
}

// cappedBySignature returns a new list keeping at most caps[sig] items of each
// signature, preserving sort order. Signatures absent from caps are dropped.
func (l *itemList) cappedBySignature(caps map[itemSignature]int) *itemList {
	l.ensureSorted()
	out := make([]Item, 0, len(l.list))
	used := make(map[itemSignature]int, len(caps))
	for _, item := range l.list {
		sig := signatureOf(item)
		if used[sig] < caps[sig] {
			used[sig]++
			out = append(out, item)
		}
	}
	return &itemList{list: out, isSorted: true}
}

// removeSignatureMultiset removes toRemove[sig] items of each signature. Items
// of the same signature are interchangeable, so any matching copies are removed
// and the remaining list stays sorted.
func (l *itemList) removeSignatureMultiset(toRemove map[itemSignature]int) {
	if len(toRemove) == 0 {
		return
	}
	l.ensureSorted()
	remaining := make(map[itemSignature]int, len(toRemove))
	for sig, n := range toRemove {
		remaining[sig] = n
	}
	out := l.list[:0]
	for _, item := range l.list {
		sig := signatureOf(item)
		if remaining[sig] > 0 {
			remaining[sig]--
			continue
		}
		out = append(out, item)
	}
	l.list = out
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
