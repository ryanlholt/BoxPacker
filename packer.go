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
	boxSorter            PackedBoxSorter
}

// NewPacker creates an empty Packer. The quantity short-circuit optimisation
// is enabled by default.
func NewPacker() *Packer {
	return &Packer{
		items:                &itemList{},
		boxQuantities:        map[Box]int{},
		quantityShortCircuit: true,
		boxSorter:            defaultPackedBoxSorter{},
	}
}

// SetPackedBoxSorter replaces the strategy used to choose the best box at each
// packing iteration, letting callers optimise for a custom objective such as
// minimising billable shipping weight (see BillableWeight). Passing nil
// restores the default ordering (most items, then fullest).
//
// Note on the quantity short-circuit: its guarantee that the result is
// identical to packing without the optimisation was established for the default
// objective, which never prefers a box that holds fewer items. A custom sorter
// that can prefer a less-full box of the same type may make the short-circuit's
// replicated solution differ from a full re-evaluation - every box produced is
// still a valid packing of real items, but if you need exact parity with the
// non-optimised result under such an objective, disable it with
// SetQuantityShortCircuit(false).
func (p *Packer) SetPackedBoxSorter(sorter PackedBoxSorter) {
	if sorter == nil {
		sorter = defaultPackedBoxSorter{}
	}
	p.boxSorter = sorter
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
// optimisation. When enabled (the default), each box evaluation only considers
// as many of each item type as that box could physically hold, and once a box
// has been solved its exact item makeup is replicated for as long as the pool
// can supply more identical boxfuls - instead of re-solving from scratch. This
// applies to a winning box made up of a mix of item types, not just a single
// type, so packing a large quantity of several different items costs roughly
// the same as packing the handful of distinct box layouts they produce rather
// than scaling with the total quantity.
func (p *Packer) SetQuantityShortCircuit(enabled bool) {
	p.quantityShortCircuit = enabled
}

// UnpackedItems returns the items that have not (yet) been packed.
func (p *Packer) UnpackedItems() []Item {
	return p.items.toSlice()
}

// Pack packs the items into boxes and returns the packed boxes.
func (p *Packer) Pack() ([]*PackedBox, error) {
	if p.boxSorter == nil {
		p.boxSorter = defaultPackedBoxSorter{}
	}

	var packedBoxes []*PackedBox

	// Keep going until everything is packed
	for p.items.count() > 0 {
		// Evaluate every candidate box type in parallel - each volume packer
		// works on its own clone of the item list, so evaluations are
		// independent. Results are kept in candidate order (smallest box
		// first) so that tie-breaking matches sequential evaluation.
		candidates := p.candidateBoxes()
		p.items.ensureSorted() // so the per-candidate clones don't each re-sort
		signatureCounts := p.items.signatureCounts()
		results := make([]*PackedBox, len(candidates))
		var wg sync.WaitGroup
		for i, box := range candidates {
			// clone on this goroutine: lazy sorting makes clones of the
			// shared list unsafe to take concurrently
			packer := newVolumePacker(box, p.itemsForBoxEvaluation(box, signatureCounts))
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
		sort.SliceStable(iteration, func(i, j int) bool { return p.boxSorter.Compare(iteration[i], iteration[j]) < 0 })
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

// replicateIdenticalBoxes is the large-quantity short-circuit. The box just
// packed is the winner of a full evaluation of the current pool. As long as the
// pool can supply another full copy of that box's exact item makeup - with
// strictly more than one boxful of every component item type still remaining -
// re-evaluating would deterministically reproduce the very same box, because no
// box can pack more items than were available a moment ago and every losing box
// can only pack fewer once items are removed. So the winning configuration is
// replicated directly instead of being re-solved.
//
// This handles a winning box made up of a mix of different item types, not just
// a single type: the multiset of item signatures is what gets replicated.
//
// Replication stops while strictly more than one boxful of any component
// remains: the final box may be only partially full, and a smaller box type
// might suit the remainder better, so the tail goes through normal evaluation.
func (p *Packer) replicateIdenticalBoxes(template *PackedBox) []*PackedBox {
	perBox := len(template.Items)
	if perBox == 0 || p.boxQuantities[template.Box] <= 0 {
		return nil
	}

	// Multiset of item signatures making up the just-packed box.
	boxCounts := make(map[itemSignature]int, 4)
	for _, item := range template.Items {
		boxCounts[signatureOf(item.Item)]++
	}
	poolCounts := p.items.signatureCounts()

	// How many further identical boxes the pool can supply while still leaving
	// strictly more than one boxful of every component for the tail.
	replications := p.boxQuantities[template.Box]
	for sig, need := range boxCounts {
		have := poolCounts[sig]
		if have <= need {
			return nil
		}
		// largest k with have-(k-1)*need > need, i.e. k boxfuls can be carved
		// out and more than one boxful still remains.
		if k := (have-need-1)/need + 1; k < replications {
			replications = k
		}
	}
	if replications <= 0 {
		return nil
	}

	clones := make([]*PackedBox, replications)
	for i := range clones {
		clones[i] = template.clone()
	}
	p.boxQuantities[template.Box] -= replications

	// Remove replications copies of the box makeup from the pool. When the box
	// holds a single item type that leads the sorted pool, those items are the
	// sorted prefix and can be dropped cheaply; otherwise fall back to a
	// signature-aware removal.
	if sig, uniform := uniformPackedSignature(template.Items); uniform && signatureOf(p.items.top()) == sig {
		p.items.removeFirstN(perBox * replications)
	} else {
		toRemove := make(map[itemSignature]int, len(boxCounts))
		for sig, need := range boxCounts {
			toRemove[sig] = need * replications
		}
		p.items.removeSignatureMultiset(toRemove)
	}
	return clones
}

// itemsForBoxEvaluation bounds the work done per box evaluation. A box can never
// hold more items of a given type than its volume and weight allow, so for each
// distinct item type only that many copies need to be handed to the packer -
// the total remaining quantity is irrelevant to how one box packs. Capping per
// signature keeps each evaluation cheap even when the pool holds a large mix of
// different item types. signatureCounts is the pool's precomputed
// signature->count map, shared across all candidate boxes in this iteration.
func (p *Packer) itemsForBoxEvaluation(box Box, signatureCounts map[itemSignature]int) *itemList {
	if !p.quantityShortCircuit || len(signatureCounts) == 0 {
		return p.items
	}

	innerVolume := boxInnerVolume(box)
	netWeight := box.MaxWeight() - box.EmptyWeight()

	caps := make(map[itemSignature]int, len(signatureCounts))
	needsCap := false
	for sig, have := range signatureCounts {
		unitVolume := maxInt(sig.width*sig.length*sig.depth, 1)
		capacity := innerVolume / unitVolume
		if sig.weight > 0 {
			capacity = minInt(capacity, netWeight/sig.weight)
		}
		if capacity < 0 {
			capacity = 0
		}
		caps[sig] = capacity
		if capacity < have {
			needsCap = true
		}
	}
	if !needsCap {
		return p.items
	}
	return p.items.cappedBySignature(caps)
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
