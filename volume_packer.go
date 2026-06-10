package boxpacker

// VolumePacker packs as many items as possible into a single specific box.
type volumePacker struct {
	box                 Box
	items               *itemList
	layerPacker         *layerPacker
	singlePassMode      bool
	packAcrossWidthOnly bool
	hasNoRotationItems  bool
}

func newVolumePacker(box Box, items *itemList) *volumePacker {
	items = items.clone()
	return &volumePacker{
		box:                box,
		items:              items,
		layerPacker:        newLayerPacker(box),
		hasNoRotationItems: items.hasNoRotationItems(),
	}
}

// NewVolumePacker creates a packer that packs as many of the given items as
// possible into the one specific box.
func NewVolumePacker(box Box, items []Item) *VolumePacker {
	return &VolumePacker{inner: newVolumePacker(box, newItemListFromSlice(items, false))}
}

// VolumePacker packs as many items as possible into a single specific box.
type VolumePacker struct {
	inner *volumePacker
}

// Pack runs the packing and returns the resulting packed box.
func (vp *VolumePacker) Pack() *PackedBox {
	return vp.inner.pack()
}

// setSinglePassMode puts the packer into a cheaper, non-exhaustive mode used
// for lookahead approximations.
func (vp *volumePacker) setSinglePassMode(singlePassMode bool) {
	vp.singlePassMode = singlePassMode
	if singlePassMode {
		vp.packAcrossWidthOnly = true
	}
	vp.layerPacker.factory.singlePassMode = singlePassMode
}

// pack as many items as possible into the box.
func (vp *volumePacker) pack() *PackedBox {
	if vp.items.count() == 0 {
		return newPackedBox(vp.box, &packedItemList{})
	}

	// Sometimes "space available" decisions depend on orientation of the box, so try both ways
	rotationsToTest := []bool{false}
	if !vp.packAcrossWidthOnly && !vp.hasNoRotationItems {
		rotationsToTest = append(rotationsToTest, true)
	}

	// The orientation of the first item can have an outsized effect on the
	// rest of the placement, so special-case that and try everything.
	var best *PackedBox
	for _, rotated := range rotationsToTest {
		boxWidth, boxLength := vp.box.InnerWidth(), vp.box.InnerLength()
		if rotated {
			boxWidth, boxLength = boxLength, boxWidth
		}

		firstItemOrientations := []*orientatedItem{nil}
		if !vp.singlePassMode {
			if possible := vp.layerPacker.factory.getPossibleOrientations(vp.items.top(), nil, boxWidth, boxLength, vp.box.InnerDepth()); len(possible) > 0 {
				firstItemOrientations = possible
			}
		}

		for _, firstItemOrientation := range firstItemOrientations {
			result := vp.packRotation(boxWidth, boxLength, firstItemOrientation)
			if len(result.Items) == vp.items.count() { // everything fitted, no need to try harder
				return result
			}
			if best == nil || result.VolumeUtilisation() > best.VolumeUtilisation() {
				best = result
			}
		}
	}

	return best
}

func (vp *volumePacker) packRotation(boxWidth, boxLength int, firstItemOrientation *orientatedItem) *PackedBox {
	var layers []*packedLayer
	items := vp.items.clone()

	for items.count() > 0 {
		layerStartDepth := 0
		for _, layer := range layers {
			layerStartDepth += layer.depth()
		}
		packedItemList := collectPackedItems(layers)

		if packedItemList.count() > 0 {
			firstItemOrientation = nil
		}

		// do a preliminary layer pack to get the depth used
		preliminaryItems := items.clone()
		preliminaryLayer := vp.layerPacker.packLayer(preliminaryItems, packedItemList.clone(), 0, 0, layerStartDepth, boxWidth, boxLength, vp.box.InnerDepth()-layerStartDepth, 0, true, firstItemOrientation)
		if len(preliminaryLayer.items) == 0 {
			break
		}

		preliminaryLayerDepth := preliminaryLayer.depth()
		if preliminaryLayerDepth == preliminaryLayer.items[0].Depth { // preliminary === final
			layers = append(layers, preliminaryLayer)
			items = preliminaryItems
		} else { // redo with now-known depth so that we can stack to that height from the first item
			layers = append(layers, vp.layerPacker.packLayer(items, packedItemList, 0, 0, layerStartDepth, boxWidth, boxLength, vp.box.InnerDepth()-layerStartDepth, preliminaryLayerDepth, true, firstItemOrientation))
		}
	}

	if !vp.singlePassMode && len(layers) > 0 {
		layers = stabiliseLayers(layers)

		// having packed layers, there may be tall, narrow gaps at the ends that can be utilised
		maxLayerWidth := 0
		for _, layer := range layers {
			maxLayerWidth = maxInt(maxLayerWidth, layer.endX())
		}
		layers = append(layers, vp.layerPacker.packLayer(items, collectPackedItems(layers), maxLayerWidth, 0, 0, boxWidth, boxLength, vp.box.InnerDepth(), vp.box.InnerDepth(), false, nil))

		maxLayerLength := 0
		for _, layer := range layers {
			maxLayerLength = maxInt(maxLayerLength, layer.endY())
		}
		layers = append(layers, vp.layerPacker.packLayer(items, collectPackedItems(layers), 0, maxLayerLength, 0, boxWidth, boxLength, vp.box.InnerDepth(), vp.box.InnerDepth(), false, nil))
	}

	if vp.box.InnerWidth() != boxWidth { // swap back width/length of the packed items to match the box
		layers = rotateLayersBack(layers)
	}

	return newPackedBox(vp.box, collectPackedItems(layers))
}

func collectPackedItems(layers []*packedLayer) *packedItemList {
	list := &packedItemList{}
	for _, layer := range layers {
		for _, item := range layer.items {
			list.insert(item)
		}
	}
	return list
}

func rotateLayersBack(layers []*packedLayer) []*packedLayer {
	rotated := make([]*packedLayer, 0, len(layers))
	for _, layer := range layers {
		newLayer := &packedLayer{}
		for _, item := range layer.items {
			newLayer.insert(&PackedItem{
				Item:   item.Item,
				X:      item.Y,
				Y:      item.X,
				Z:      item.Z,
				Width:  item.Length,
				Length: item.Width,
				Depth:  item.Depth,
			})
		}
		rotated = append(rotated, newLayer)
	}
	return rotated
}
