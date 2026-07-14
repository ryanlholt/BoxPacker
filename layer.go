package boxpacker

import "sort"

// packedLayer is a horizontal slice of packed items within a box.
type packedLayer struct {
	items []*PackedItem
}

func (l *packedLayer) insert(item *PackedItem) {
	l.items = append(l.items, item)
}

func (l *packedLayer) merge(other *packedLayer) {
	l.items = append(l.items, other.items...)
}

func (l *packedLayer) footprint() int {
	return l.width() * l.length()
}

func (l *packedLayer) endX() int {
	end := 0
	for _, item := range l.items {
		end = maxInt(end, item.X+item.Width)
	}
	return end
}

func (l *packedLayer) endY() int {
	end := 0
	for _, item := range l.items {
		end = maxInt(end, item.Y+item.Length)
	}
	return end
}

func (l *packedLayer) startZ() int {
	if len(l.items) == 0 {
		return 0
	}
	start := l.items[0].Z
	for _, item := range l.items[1:] {
		start = minInt(start, item.Z)
	}
	return start
}

func (l *packedLayer) width() int {
	if len(l.items) == 0 {
		return 0
	}
	start, end := l.items[0].X, l.items[0].X+l.items[0].Width
	for _, item := range l.items[1:] {
		start = minInt(start, item.X)
		end = maxInt(end, item.X+item.Width)
	}
	return end - start
}

func (l *packedLayer) length() int {
	if len(l.items) == 0 {
		return 0
	}
	start, end := l.items[0].Y, l.items[0].Y+l.items[0].Length
	for _, item := range l.items[1:] {
		start = minInt(start, item.Y)
		end = maxInt(end, item.Y+item.Length)
	}
	return end - start
}

func (l *packedLayer) depth() int {
	if len(l.items) == 0 {
		return 0
	}
	start, end := l.items[0].Z, l.items[0].Z+l.items[0].Depth
	for _, item := range l.items[1:] {
		start = minInt(start, item.Z)
		end = maxInt(end, item.Z+item.Depth)
	}
	return end - start
}

// layerPacker packs items into an individual vertical layer of a box.
type layerPacker struct {
	box     Box
	factory *orientatedItemFactory
}

func newLayerPacker(box Box) *layerPacker {
	return &layerPacker{box: box, factory: &orientatedItemFactory{box: box}}
}

// packLayer packs items into a layer starting at (startX, startY, startZ).
// widthForLayer and lengthForLayer are absolute bounds (not remaining space);
// depthForLayer is the height available to this layer. guidelineLayerDepth,
// when non-zero, is a target depth that items may be stacked up to.
//
// The items list is mutated: successfully packed items are consumed, items
// that did not fit are left in the list.
func (lp *layerPacker) packLayer(
	items *itemList,
	packedItemList *packedItemList,
	startX, startY, startZ int,
	widthForLayer, lengthForLayer, depthForLayer int,
	guidelineLayerDepth int,
	considerStability bool,
	firstItem *orientatedItem,
) *packedLayer {
	layer := &packedLayer{}
	x, y, z := startX, startY, startZ
	rowLength := 0
	var prevItem *orientatedItem
	var skippedItems []Item

	for items.count() > 0 {
		itemToPack := items.extract()

		// skip items that will never fit e.g. too heavy
		if itemToPack.Weight() > lp.box.MaxWeight()-lp.box.EmptyWeight()-packedItemList.weight {
			continue
		}

		var orientated *orientatedItem
		if firstItem != nil && firstItem.item == itemToPack {
			orientated = firstItem
			firstItem = nil
		} else {
			orientated = lp.factory.getBestOrientation(itemToPack, prevItem, items, widthForLayer-x, lengthForLayer-y, depthForLayer, rowLength, x, y, z, packedItemList, considerStability)
		}

		if orientated != nil {
			packed := &PackedItem{Item: itemToPack, X: x, Y: y, Z: z, Width: orientated.width, Length: orientated.length, Depth: orientated.depth}
			layer.insert(packed)
			packedItemList.insert(packed)

			rowLength = maxInt(rowLength, packed.Length)
			prevItem = orientated

			// Figure out if we can stack items on top of this rather than side by side
			// e.g. when we've packed a tall item, and have just put a shorter one next to it.
			guideline := guidelineLayerDepth
			if guideline == 0 {
				guideline = layer.depth()
			}
			stackableDepth := guideline - packed.Depth
			if stackableDepth > 0 {
				stackedLayer := lp.packLayer(items, packedItemList, x, y, z+packed.Depth, x+packed.Width, y+packed.Length, stackableDepth, stackableDepth, considerStability, nil)
				layer.merge(stackedLayer)
			}

			x += packed.Width

			// might be space available lengthwise across the width of this item, up to the current layer length
			layer.merge(lp.packLayer(items, packedItemList, x-packed.Width, y+packed.Length, z, x, y+rowLength, depthForLayer, layer.depth(), considerStability, nil))

			if items.count() == 0 && len(skippedItems) > 0 {
				items.replace(skippedItems, true)
				skippedItems = nil
			}
			continue
		}

		if items.count() > 0 { // skip for now, move on to the next item
			skippedItems = append(skippedItems, itemToPack)
			// abandon here if next item is the same, no point trying to keep going.
			// Last one is not skipped, need that to trigger appropriate reset logic.
			for items.count() > 1 && isSameDimensions(itemToPack, items.top()) {
				skippedItems = append(skippedItems, items.extract())
			}
			continue
		}

		if x > startX { // no more fit width-wise, reset for a new row
			y += rowLength
			x = startX
			rowLength = 0
			skippedItems = append(skippedItems, itemToPack)
			items.replace(append(skippedItems, items.toSlice()...), true)
			skippedItems = nil
			prevItem = nil
			continue
		}

		// no items fit at all, the next vertical layer is the caller's responsibility
		skippedItems = append(skippedItems, itemToPack)
		items.replace(append(skippedItems, items.toSlice()...), true)
		return layer
	}

	return layer
}

// stabiliseLayers reorders layers so the ones with the greatest surface area
// are at the bottom, recalculating item z positions to suit. During packing
// it is quite possible that layers are created that overhang the ones below.
func stabiliseLayers(layers []*packedLayer) []*packedLayer {
	sorted := make([]*packedLayer, len(layers))
	copy(sorted, layers)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].footprint() != sorted[j].footprint() {
			return sorted[i].footprint() > sorted[j].footprint()
		}
		return sorted[i].depth() > sorted[j].depth()
	})

	stabilised := make([]*packedLayer, 0, len(sorted))
	currentZ := 0
	for _, oldLayer := range sorted {
		oldZStart := oldLayer.startZ()
		newLayer := &packedLayer{}
		for _, item := range oldLayer.items {
			newLayer.insert(&PackedItem{
				Item:   item.Item,
				X:      item.X,
				Y:      item.Y,
				Z:      item.Z - oldZStart + currentZ,
				Width:  item.Width,
				Length: item.Length,
				Depth:  item.Depth,
			})
		}
		stabilised = append(stabilised, newLayer)
		currentZ += newLayer.depth()
	}
	return stabilised
}
