package boxpacker

import "sort"

// weightRedistributor mirrors the PHP 3.x post-pack pass. It considers pairs
// of boxes from heaviest to lightest and moves items only when doing so lowers
// item-weight variance and both resulting item sets can still be packed into a
// single available box.
type weightRedistributor struct {
	boxes         []Box
	boxSorter     PackedBoxSorter
	boxQuantities map[Box]int
}

func newWeightRedistributor(boxes []Box, sorter PackedBoxSorter, boxQuantities map[Box]int) *weightRedistributor {
	if sorter == nil {
		sorter = defaultPackedBoxSorter{}
	}
	return &weightRedistributor{
		boxes:         boxes,
		boxSorter:     sorter,
		boxQuantities: boxQuantities,
	}
}

func (r *weightRedistributor) redistributeWeight(original []*PackedBox) []*PackedBox {
	if len(original) < 2 {
		return original
	}

	targetWeight := meanItemWeight(original)
	boxes := append([]*PackedBox(nil), original...)
	sort.SliceStable(boxes, func(i, j int) bool { return boxes[i].Weight() > boxes[j].Weight() })

	for {
		iterationSuccessful := false
		for a := 0; a < len(boxes) && !iterationSuccessful; a++ {
			for b := a + 1; b < len(boxes); b++ {
				if boxes[a].Weight() == boxes[b].Weight() {
					continue
				}

				newA, newB, changed := r.equaliseWeight(boxes[a], boxes[b], targetWeight)
				if !changed {
					continue
				}
				boxes[a], boxes[b] = newA, newB
				boxes = removeNilPackedBoxes(boxes)
				iterationSuccessful = true
				break
			}
		}
		if !iterationSuccessful {
			break
		}
	}

	// PHP returns a PackedBoxList configured with the active sorter. A Go slice
	// has no lazy ordering, so apply that ordering before returning it.
	sort.SliceStable(boxes, func(i, j int) bool { return r.boxSorter.Compare(boxes[i], boxes[j]) < 0 })
	return boxes
}

func (r *weightRedistributor) equaliseWeight(boxA, boxB *PackedBox, targetWeight float64) (*PackedBox, *PackedBox, bool) {
	// redistributeWeight initially sorts by gross weight and maintains the
	// heavier replacement in the earlier slot after every successful move.
	if boxB.Weight() > boxA.Weight() {
		boxA, boxB = boxB, boxA
	}
	overWeightBox, underWeightBox := boxA, boxB
	overWeightItems := packedBoxItems(overWeightBox)
	underWeightItems := packedBoxItems(underWeightBox)
	anyIterationSuccessful := false

	for key := 0; key < len(overWeightItems); {
		overWeightItem := overWeightItems[key]
		if !wouldRepackActuallyHelp(overWeightItems, overWeightItem, underWeightItems, targetWeight) {
			key++
			continue
		}

		newUnderWeightItems := append(append([]Item(nil), underWeightItems...), overWeightItem)
		newLighterBoxes := r.doVolumeRepack(newUnderWeightItems, underWeightBox.Box)
		if len(newLighterBoxes) != 1 {
			key++
			continue
		}
		newLighterBox := newLighterBoxes[0]

		if len(overWeightItems) == 1 {
			changes := make(map[Box]int, 3)
			addBoxQuantityChange(changes, overWeightBox.Box, 1)
			addBoxQuantityChange(changes, underWeightBox.Box, 1)
			addBoxQuantityChange(changes, newLighterBox.Box, -1)
			if !r.applyBoxQuantityChanges(changes) {
				key++
				continue
			}
			return nil, newLighterBox, true
		}

		newOverWeightItems := make([]Item, 0, len(overWeightItems)-1)
		newOverWeightItems = append(newOverWeightItems, overWeightItems[:key]...)
		newOverWeightItems = append(newOverWeightItems, overWeightItems[key+1:]...)
		newHeavierBoxes := r.doVolumeRepack(newOverWeightItems, overWeightBox.Box)
		if len(newHeavierBoxes) != 1 {
			// PHP keeps these tentative array edits even though it has not yet
			// replaced either packed box. A later item can therefore make the
			// cumulative transfer repackable. Although the reference source calls
			// this branch theoretically impossible, real parity fixtures exercise
			// it, so preserve the state transition exactly.
			overWeightItems = newOverWeightItems
			underWeightItems = newUnderWeightItems
			continue
		}
		newHeavierBox := newHeavierBoxes[0]

		changes := make(map[Box]int, 4)
		addBoxQuantityChange(changes, overWeightBox.Box, 1)
		addBoxQuantityChange(changes, underWeightBox.Box, 1)
		addBoxQuantityChange(changes, newHeavierBox.Box, -1)
		addBoxQuantityChange(changes, newLighterBox.Box, -1)
		if !r.applyBoxQuantityChanges(changes) {
			key++
			continue
		}

		overWeightItems = newOverWeightItems
		underWeightItems = newUnderWeightItems
		overWeightBox, underWeightBox = newHeavierBox, newLighterBox
		boxA, boxB = newHeavierBox, newLighterBox
		anyIterationSuccessful = true
		// The next original item shifted into this index when the moved item was
		// removed, matching PHP foreach behavior after unset(current).
	}

	return boxA, boxB, anyIterationSuccessful
}

func (r *weightRedistributor) doVolumeRepack(items []Item, currentBox Box) []*PackedBox {
	packer := NewPacker()
	packer.boxes = append([]Box(nil), r.boxes...)
	packer.boxQuantities = make(map[Box]int, len(r.boxQuantities))
	for box, quantity := range r.boxQuantities {
		packer.boxQuantities[box] = quantity
	}
	// The current box is already consumed by the solution under consideration;
	// make it available to this prospective replacement pack.
	packer.boxQuantities[currentBox]++
	packer.items = newItemListFromSlice(items, false)

	packed, _ := packer.packBasic(true)
	return packed
}

func (r *weightRedistributor) applyBoxQuantityChanges(changes map[Box]int) bool {
	for box, change := range changes {
		if r.boxQuantities[box]+change < 0 {
			return false
		}
	}
	for box, change := range changes {
		r.boxQuantities[box] += change
	}
	return true
}

func addBoxQuantityChange(changes map[Box]int, box Box, change int) {
	changes[box] += change
}

func packedBoxItems(box *PackedBox) []Item {
	items := make([]Item, len(box.Items))
	for i, packedItem := range box.Items {
		items[i] = packedItem.Item
	}
	// PHP's basic pack removes items by iterating PackedItemList, which lazily
	// sorts that list by original volume and then weight. WeightRedistributor
	// later receives that sorted internal order even though placement order may
	// differ. Reproduce it locally without changing Go's public Items ordering.
	sort.SliceStable(items, func(i, j int) bool {
		iv, jv := itemVolume(items[i]), itemVolume(items[j])
		if iv != jv {
			return iv > jv
		}
		return items[i].Weight() > items[j].Weight()
	})
	return items
}

func meanItemWeight(boxes []*PackedBox) float64 {
	weight := 0
	for _, box := range boxes {
		weight += box.ItemWeight()
	}
	return float64(weight) / float64(len(boxes))
}

func wouldRepackActuallyHelp(overWeightItems []Item, overWeightItem Item, underWeightItems []Item, targetWeight float64) bool {
	overWeight := itemSliceWeight(overWeightItems)
	underWeight := itemSliceWeight(underWeightItems)
	if float64(overWeightItem.Weight()+underWeight) > targetWeight {
		return false
	}

	oldVariance := twoBoxWeightVariance(overWeight, underWeight)
	newVariance := twoBoxWeightVariance(overWeight-overWeightItem.Weight(), underWeight+overWeightItem.Weight())
	return newVariance < oldVariance
}

func itemSliceWeight(items []Item) int {
	weight := 0
	for _, item := range items {
		weight += item.Weight()
	}
	return weight
}

func twoBoxWeightVariance(boxAWeight, boxBWeight int) float64 {
	mean := float64(boxAWeight+boxBWeight) / 2
	difference := float64(boxAWeight) - mean
	return difference * difference
}

func removeNilPackedBoxes(boxes []*PackedBox) []*PackedBox {
	filtered := boxes[:0]
	for _, box := range boxes {
		if box != nil {
			filtered = append(filtered, box)
		}
	}
	return filtered
}
