package boxpacker

import (
	"math"
	"strconv"
	"strings"
)

// orientatedItem is an item in a specific orientation.
type orientatedItem struct {
	item                 Item
	width, length, depth int
}

func (o *orientatedItem) surfaceFootprint() int {
	return o.width * o.length
}

// isStable reports whether the item has a low centre of gravity in this
// orientation, calculated as a tipping point of >15 degrees. Assumes equal
// weight distribution.
func (o *orientatedItem) isStable() bool {
	depth := o.depth
	if depth == 0 {
		depth = 1
	}
	return math.Atan(float64(minInt(o.length, o.width))/float64(depth)) > 0.261
}

func (o *orientatedItem) isSameDimensions(item Item) bool {
	if o.item == item {
		return true
	}
	return sortedDims(o.width, o.length, o.depth) == sortedDims(item.Width(), item.Length(), item.Depth())
}

// emptyBoxStableCache caches whether an item has any stable orientation in an
// otherwise empty box. Keyed by item dimensions/rotation and box dimensions.
var emptyBoxStableCache = newBoundedCache(1024)

// orientatedItemFactory works out which orientations an item can be placed in
// within a given box, and which of those is best for a given context.
type orientatedItemFactory struct {
	box            Box
	singlePassMode bool
}

// getBestOrientation returns the best orientation for an item in the
// available space, or nil if it cannot fit at all.
func (f *orientatedItemFactory) getBestOrientation(
	item Item,
	prevItem *orientatedItem,
	nextItems *itemList,
	widthLeft, lengthLeft, depthLeft int,
	rowLength int,
	x, y, z int,
	prevPackedItemList *packedItemList,
	considerStability bool,
) *orientatedItem {
	possibleOrientations := f.getPossibleOrientations(item, prevItem, widthLeft, lengthLeft, depthLeft)

	usableOrientations := possibleOrientations
	if considerStability {
		usableOrientations = f.getUsableOrientations(item, possibleOrientations)
	}
	if len(usableOrientations) == 0 {
		return nil
	}

	sorter := &orientatedItemSorter{
		factory:            f,
		singlePassMode:     f.singlePassMode,
		widthLeft:          widthLeft,
		lengthLeft:         lengthLeft,
		depthLeft:          depthLeft,
		nextItems:          nextItems,
		rowLength:          rowLength,
		x:                  x,
		y:                  y,
		z:                  z,
		prevPackedItemList: prevPackedItemList,
	}

	best := usableOrientations[0]
	for _, candidate := range usableOrientations[1:] {
		if sorter.compare(candidate, best) < 0 {
			best = candidate
		}
	}
	return best
}

// getPossibleOrientations finds all orientations of the item that fit within
// the given space.
func (f *orientatedItemFactory) getPossibleOrientations(
	item Item,
	prevItem *orientatedItem,
	widthLeft, lengthLeft, depthLeft int,
) []*orientatedItem {
	permutations := generatePermutations(item, prevItem)

	orientations := make([]*orientatedItem, 0, len(permutations))
	for _, dims := range permutations {
		if dims[0] <= widthLeft && dims[1] <= lengthLeft && dims[2] <= depthLeft {
			orientations = append(orientations, &orientatedItem{item: item, width: dims[0], length: dims[1], depth: dims[2]})
		}
	}
	return orientations
}

// getUsableOrientations prefers stable (low centre of gravity) orientations,
// allowing unstable ones only when the item has no stable fit even in an
// empty box.
func (f *orientatedItemFactory) getUsableOrientations(item Item, possibleOrientations []*orientatedItem) []*orientatedItem {
	var stable, unstable []*orientatedItem
	for _, orientation := range possibleOrientations {
		if orientation.isStable() || f.box.InnerDepth() == orientation.depth {
			stable = append(stable, orientation)
		} else {
			unstable = append(unstable, orientation)
		}
	}

	if len(stable) > 0 {
		return stable
	}
	if len(unstable) > 0 && !f.hasStableOrientationsInEmptyBox(item) {
		return unstable
	}
	return nil
}

func (f *orientatedItemFactory) hasStableOrientationsInEmptyBox(item Item) bool {
	var key strings.Builder
	key.WriteString(strconv.Itoa(item.Width()))
	key.WriteByte('|')
	key.WriteString(strconv.Itoa(item.Length()))
	key.WriteByte('|')
	key.WriteString(strconv.Itoa(item.Depth()))
	key.WriteByte('|')
	key.WriteString(item.AllowedRotation().String())
	key.WriteByte('|')
	key.WriteString(strconv.Itoa(f.box.InnerWidth()))
	key.WriteByte('|')
	key.WriteString(strconv.Itoa(f.box.InnerLength()))
	key.WriteByte('|')
	key.WriteString(strconv.Itoa(f.box.InnerDepth()))

	if cached, ok := emptyBoxStableCache.Load(key.String()); ok {
		return cached.(bool)
	}

	orientations := f.getPossibleOrientations(item, nil, f.box.InnerWidth(), f.box.InnerLength(), f.box.InnerDepth())
	hasStable := false
	for _, orientation := range orientations {
		if orientation.isStable() {
			hasStable = true
			break
		}
	}
	emptyBoxStableCache.Store(key.String(), hasStable)
	return hasStable
}

func generatePermutations(item Item, prevItem *orientatedItem) [][3]int {
	// Special case items that are the same as what we just packed - keep orientation
	if prevItem != nil && prevItem.isSameDimensions(item) && allowedOrientation(item, prevItem.width, prevItem.length, prevItem.depth) {
		return [][3]int{{prevItem.width, prevItem.length, prevItem.depth}}
	}

	w, l, d := item.Width(), item.Length(), item.Depth()

	permutations := make([][3]int, 1, 6)
	permutations[0] = [3]int{w, l, d}

	addUnique := func(dims [3]int) {
		for _, existing := range permutations {
			if existing == dims {
				return
			}
		}
		permutations = append(permutations, dims)
	}

	if item.AllowedRotation() != RotationNever { // simple 2D rotation
		addUnique([3]int{l, w, d})
	}
	if item.AllowedRotation() == RotationBestFit { // add 3D rotation if we're allowed
		addUnique([3]int{w, d, l})
		addUnique([3]int{l, d, w})
		addUnique([3]int{d, w, l})
		addUnique([3]int{d, l, w})
	}
	return permutations
}

// lookaheadCache caches forward-looking packing approximations, keyed by the
// available space and the dimensions of the upcoming items.
var lookaheadCache = newBoundedCache(4096)

// orientationLookaheadDepth is the maximum number of upcoming items used to
// score an orientation. Quantity capping must retain at least this much
// headroom beyond physical box capacity so capped and uncapped evaluations see
// the same window at every placement decision.
const orientationLookaheadDepth = 8

// orientatedItemSorter decides which of two orientations is the better choice
// for the current packing context.
type orientatedItemSorter struct {
	factory                          *orientatedItemFactory
	singlePassMode                   bool
	widthLeft, lengthLeft, depthLeft int
	nextItems                        *itemList
	rowLength                        int
	x, y, z                          int
	prevPackedItemList               *packedItemList
}

func (s *orientatedItemSorter) compare(a, b *orientatedItem) int {
	// Prefer exact fits in width/length/depth order
	aWidthLeft, bWidthLeft := s.widthLeft-a.width, s.widthLeft-b.width
	if decider := exactFitDecider(aWidthLeft, bWidthLeft); decider != 0 {
		return decider
	}

	aLengthLeft, bLengthLeft := s.lengthLeft-a.length, s.lengthLeft-b.length
	if decider := exactFitDecider(aLengthLeft, bLengthLeft); decider != 0 {
		return decider
	}

	aDepthLeft, bDepthLeft := s.depthLeft-a.depth, s.depthLeft-b.depth
	if decider := exactFitDecider(aDepthLeft, bDepthLeft); decider != 0 {
		return decider
	}

	// prefer leaving room for next item(s)
	if decider := s.lookAheadDecider(a, b, aWidthLeft, bWidthLeft); decider != 0 {
		return decider
	}

	// otherwise prefer leaving minimum possible gap, or the greatest footprint
	aMinGap := minInt(aWidthLeft, aLengthLeft)
	bMinGap := minInt(bWidthLeft, bLengthLeft)
	if aMinGap != bMinGap {
		if aMinGap < bMinGap {
			return -1
		}
		return 1
	}
	if a.surfaceFootprint() != b.surfaceFootprint() {
		if a.surfaceFootprint() < b.surfaceFootprint() {
			return -1
		}
		return 1
	}
	return 0
}

func (s *orientatedItemSorter) lookAheadDecider(a, b *orientatedItem, aWidthLeft, bWidthLeft int) int {
	if s.nextItems.count() == 0 {
		return 0
	}

	nextItem := s.nextItems.top()
	nextFitsAfterA := len(s.factory.getPossibleOrientations(nextItem, a, aWidthLeft, s.lengthLeft, s.depthLeft)) > 0
	nextFitsAfterB := len(s.factory.getPossibleOrientations(nextItem, b, bWidthLeft, s.lengthLeft, s.depthLeft)) > 0
	if nextFitsAfterA && !nextFitsAfterB {
		return -1
	}
	if nextFitsAfterB && !nextFitsAfterA {
		return 1
	}

	// if not an easy either/or, do a partial lookahead
	additionalPackedA := s.additionalItemsPacked(a)
	additionalPackedB := s.additionalItemsPacked(b)
	if additionalPackedA != additionalPackedB {
		if additionalPackedA > additionalPackedB {
			return -1
		}
		return 1
	}
	return 0
}

// additionalItemsPacked approximates how many of the upcoming items could
// still be packed alongside/after the given orientation. Not an actual
// packing - this focuses purely on fit.
func (s *orientatedItemSorter) additionalItemsPacked(prev *orientatedItem) int {
	if s.singlePassMode {
		return 0
	}

	currentRowLength := maxInt(prev.length, s.rowLength)
	itemsToPack := s.nextItems.topN(orientationLookaheadDepth) // cap lookahead as this gets recursive and slow

	var key strings.Builder
	for _, v := range []int{s.widthLeft, s.lengthLeft, prev.width, prev.length, currentRowLength, s.depthLeft} {
		key.WriteString(strconv.Itoa(v))
		key.WriteByte('|')
	}
	for _, item := range itemsToPack.toSlice() {
		for _, v := range []int{item.Width(), item.Length(), item.Depth(), item.Weight()} {
			key.WriteString(strconv.Itoa(v))
			key.WriteByte('|')
		}
		key.WriteString(item.AllowedRotation().String())
		key.WriteByte('|')
	}
	cacheKey := key.String()

	if cached, ok := lookaheadCache.Load(cacheKey); ok {
		return cached.(int)
	}

	originalCount := itemsToPack.count()

	// remainder of the current row
	rowVolume := &workingVolume{width: s.widthLeft - prev.width, length: currentRowLength, depth: s.depthLeft}
	rowPacker := newVolumePacker(rowVolume, itemsToPack)
	rowPacker.setSinglePassMode(true)
	rowPacked := rowPacker.pack()
	itemsToPack.removePackedItems(rowPacked.Items)

	// then the rest of the layer
	nextRowsVolume := &workingVolume{width: s.widthLeft, length: s.lengthLeft - currentRowLength, depth: s.depthLeft}
	nextRowsPacker := newVolumePacker(nextRowsVolume, itemsToPack)
	nextRowsPacker.setSinglePassMode(true)
	nextRowsPacked := nextRowsPacker.pack()
	itemsToPack.removePackedItems(nextRowsPacked.Items)

	packedCount := originalCount - itemsToPack.count()
	lookaheadCache.Store(cacheKey, packedCount)
	return packedCount
}

func exactFitDecider(dimensionALeft, dimensionBLeft int) int {
	if dimensionALeft == 0 && dimensionBLeft > 0 {
		return -1
	}
	if dimensionALeft > 0 && dimensionBLeft == 0 {
		return 1
	}
	return 0
}

// allowedOrientation checks the current item's policy, never its predecessor's.
func allowedOrientation(item Item, w, l, d int) bool {
	if w == item.Width() && l == item.Length() && d == item.Depth() {
		return true
	}
	if item.AllowedRotation() == RotationNever {
		return false
	}
	if d == item.Depth() && w == item.Length() && l == item.Width() {
		return true
	}
	return item.AllowedRotation() == RotationBestFit && sortedDims(w, l, d) == sortedDims(item.Width(), item.Length(), item.Depth())
}
