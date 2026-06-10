package boxpacker

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// NoBoxesAvailableError is returned by Pack when an item cannot fit into any
// available box.
type NoBoxesAvailableError struct {
	// Item is the first item that could not be packed.
	Item Item
}

func (e *NoBoxesAvailableError) Error() string {
	return fmt.Sprintf("boxpacker: no boxes could be found for item %q", e.Item.Description())
}

// Packer packs items into boxes, choosing box sizes using built-in heuristics
// for the best overall solution.
type Packer struct {
	items                *itemList
	boxes                []Box
	boxQuantities        map[Box]int
	allowPartialResults  bool
	quantityShortCircuit bool
}

// NewPacker creates an empty Packer. The quantity short-circuit optimisation
// is enabled by default.
func NewPacker() *Packer {
	return &Packer{
		items:                &itemList{},
		boxQuantities:        map[Box]int{},
		quantityShortCircuit: true,
	}
}

// AddItem adds qty copies of an item to be packed.
func (p *Packer) AddItem(item Item, qty int) {
	p.items.insert(item, qty)
}

// AddBox adds a box type that is available to pack into. If the box
// implements LimitedSupplyBox, only its available quantity will be used.
func (p *Packer) AddBox(box Box) {
	p.boxes = append(p.boxes, box)
	if limited, ok := box.(LimitedSupplyBox); ok {
		p.boxQuantities[box] = limited.QuantityAvailable()
	} else {
		p.boxQuantities[box] = math.MaxInt
	}
}

// SetBoxQuantity overrides the quantity available of a previously added box.
func (p *Packer) SetBoxQuantity(box Box, qty int) {
	p.boxQuantities[box] = qty
}

// AllowPartialResults controls behaviour when an item fits in no available
// box: when true, Pack returns the boxes it could pack without an error and
// the leftovers are available via UnpackedItems; when false (the default),
// Pack returns a *NoBoxesAvailableError.
func (p *Packer) AllowPartialResults(allow bool) {
	p.allowPartialResults = allow
}

// SetQuantityShortCircuit enables or disables the large-quantity
// optimisation. When enabled (the default) and the remaining items are all
// identical, a fully-solved box is replicated for subsequent boxes instead of
// being re-solved from scratch, so packing N identical items costs roughly
// the same as packing one box full of them rather than scaling with N.
func (p *Packer) SetQuantityShortCircuit(enabled bool) {
	p.quantityShortCircuit = enabled
}

// UnpackedItems returns the items that have not (yet) been packed.
func (p *Packer) UnpackedItems() []Item {
	return p.items.toSlice()
}

// Pack packs the items into boxes and returns the packed boxes.
func (p *Packer) Pack() ([]*PackedBox, error) {
	var packedBoxes []*PackedBox

	// Keep going until everything is packed
	for p.items.count() > 0 {
		// Evaluate every candidate box type in parallel - each volume packer
		// works on its own clone of the item list, so evaluations are
		// independent. Results are kept in candidate order (smallest box
		// first) so that tie-breaking matches sequential evaluation.
		candidates := p.candidateBoxes()
		p.items.ensureSorted() // so the per-candidate clones don't each re-sort
		results := make([]*PackedBox, len(candidates))
		var wg sync.WaitGroup
		for i, box := range candidates {
			// clone on this goroutine: lazy sorting makes clones of the
			// shared list unsafe to take concurrently
			packer := newVolumePacker(box, p.itemsForBoxEvaluation(box))
			wg.Add(1)
			go func(i int, packer *volumePacker) {
				defer wg.Done()
				results[i] = packer.pack()
			}(i, packer)
		}
		wg.Wait()

		var iteration []*PackedBox
		for _, packedBox := range results {
			if packedBox != nil && len(packedBox.Items) > 0 {
				iteration = append(iteration, packedBox)
			}
		}

		if len(iteration) == 0 {
			if p.allowPartialResults {
				break
			}
			return packedBoxes, &NoBoxesAvailableError{Item: p.items.top()}
		}

		// Find the best box of the iteration, and remove the packed items from the unpacked list
		sort.SliceStable(iteration, func(i, j int) bool { return comparePackedBoxes(iteration[i], iteration[j]) < 0 })
		best := iteration[0]

		p.items.removePackedItems(best.Items)
		packedBoxes = append(packedBoxes, best)
		p.boxQuantities[best.Box]--

		if p.quantityShortCircuit {
			packedBoxes = append(packedBoxes, p.replicateIdenticalBoxes(best)...)
		}
	}

	return packedBoxes, nil
}

// replicateIdenticalBoxes is the large-quantity short-circuit. When a box has
// just been packed full of a single item type and only more of that same item
// type remains, every box type would pack exactly as it did in the iteration
// just evaluated, so the winning configuration is replicated directly instead
// of being re-solved.
//
// Replication stops while strictly more than one boxful remains: the final
// box may be only partially full, and a smaller box type might suit the
// remainder better, so the tail goes through normal evaluation.
func (p *Packer) replicateIdenticalBoxes(template *PackedBox) []*PackedBox {
	perBox := len(template.Items)
	if perBox == 0 || p.items.count() <= perBox {
		return nil
	}

	boxSignature, boxUniform := uniformPackedSignature(template.Items)
	if !boxUniform {
		return nil
	}
	remainingSignature, remainingUniform := p.items.uniformSignature()
	if !remainingUniform || boxSignature != remainingSignature {
		return nil
	}

	var clones []*PackedBox
	for p.items.count() > perBox && p.boxQuantities[template.Box] > 0 {
		clones = append(clones, template.clone())
		p.boxQuantities[template.Box]--
		p.items.removeFirstN(perBox)
	}
	return clones
}

// itemsForBoxEvaluation bounds the work done per box evaluation. When the
// remaining items are all identical, a box can never hold more of them than
// its volume and weight capacity allow, so only that many items need to be
// considered - the total remaining quantity is irrelevant to how one box
// packs.
func (p *Packer) itemsForBoxEvaluation(box Box) *itemList {
	if !p.quantityShortCircuit {
		return p.items
	}
	signature, uniform := p.items.uniformSignature()
	if !uniform {
		return p.items
	}

	unitVolume := maxInt(signature.width*signature.length*signature.depth, 1)
	capacity := boxInnerVolume(box) / unitVolume
	if signature.weight > 0 {
		capacity = minInt(capacity, (box.MaxWeight()-box.EmptyWeight())/signature.weight)
	}
	if capacity < p.items.count() {
		return p.items.topN(capacity)
	}
	return p.items
}

// candidateBoxes returns a "smart" ordering of the boxes to try packing items
// into: smallest first, but boxes that cannot possibly hold the entire
// remaining set of items by volume are evaluated last.
func (p *Packer) candidateBoxes() []Box {
	sorted := make([]Box, len(p.boxes))
	copy(sorted, p.boxes)
	sort.SliceStable(sorted, func(i, j int) bool { return compareBoxes(sorted[i], sorted[j]) < 0 })

	remainingVolume := p.items.totalVolume()

	var preferred, other []Box
	for _, box := range sorted {
		if p.boxQuantities[box] <= 0 {
			continue
		}
		if boxInnerVolume(box) >= remainingVolume {
			preferred = append(preferred, box)
		} else {
			other = append(other, box)
		}
	}
	return append(preferred, other...)
}

func uniformPackedSignature(items []*PackedItem) (itemSignature, bool) {
	if len(items) == 0 {
		return itemSignature{}, false
	}
	signature := signatureOf(items[0].Item)
	for _, item := range items[1:] {
		if signatureOf(item.Item) != signature {
			return itemSignature{}, false
		}
	}
	return signature, true
}
