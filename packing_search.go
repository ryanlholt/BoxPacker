package boxpacker

import (
	"math/bits"
	"sort"
)

const maximumPackingSearchItems = 16

// SetPackingSearchBudget enables an optional post-pack search for fewer boxes,
// then less total outer volume. The budget limits additional single-box solves;
// coverage search and subset screening each use at most
// 64 * min(evaluations, 4096) steps. Zero (the default)
// disables it. The search runs only for complete orders of at most 16 items with
// the built-in sorter, and respects the original box supply. It returns the best
// feasible result found, without claiming global optimality.
func (p *Packer) SetPackingSearchBudget(evaluations int) {
	p.packingSearchBudget = maxInt(0, evaluations)
}

type searchPattern struct {
	mask        uint32
	box         *PackedBox
	outerVolume int
}

// searchPacking keeps original identities attached to each pattern. Unlike the
// analysis harness, it uses exact, disjoint item coverage rather than dropping
// excess demand from layouts. Removing supporting items is therefore unnecessary.
func (p *Packer) searchPacking(original *itemList, supply map[Box]int, incumbent []*PackedBox) []*PackedBox {
	items := original.toSlice()
	full := uint32(1<<len(items)) - 1
	patterns := make([]searchPattern, 0)
	seen := make(map[Box]map[uint32]bool)
	add := func(box *PackedBox, available uint32) {
		mask := uint32(0)
		for _, placement := range box.Items {
			for i, item := range items {
				bit := uint32(1 << i)
				if available&bit != 0 && mask&bit == 0 && placement.Item == item {
					mask |= bit
					break
				}
			}
		}
		if mask == 0 {
			return
		}
		if seen[box.Box] == nil {
			seen[box.Box] = make(map[uint32]bool)
		}
		if seen[box.Box][mask] {
			return
		}
		seen[box.Box][mask] = true
		patterns = append(patterns, searchPattern{mask, box, box.Box.OuterWidth() * box.Box.OuterLength() * box.Box.OuterDepth()})
	}
	remaining := full
	bestVolume := 0
	for _, box := range incumbent {
		before := len(patterns)
		add(box, remaining)
		remaining &^= patterns[before].mask
		bestVolume += patterns[before].outerVolume
	}

	// Larger subsets first usually find useful combinations with a small budget.
	masks := make([]uint32, int(full))
	for i := range masks {
		masks[i] = uint32(i + 1)
	}
	sort.Slice(masks, func(i, j int) bool {
		a, b := bits.OnesCount32(masks[i]), bits.OnesCount32(masks[j])
		if a != b {
			return a > b
		}
		return masks[i] < masks[j]
	})
	boxes := append([]Box(nil), p.boxes...)
	sort.SliceStable(boxes, func(i, j int) bool { return compareBoxes(boxes[i], boxes[j]) < 0 })
	budget := p.packingSearchBudget
	// Bound subset screening too, including masks rejected without a solve.
	// Saturation avoids integer overflow for very large caller-supplied budgets.
	nodeLimit := minInt(budget, 4096) * 64
	screened := 0
searchGeneration:
	for _, mask := range masks {
		selected := make([]Item, 0, bits.OnesCount32(mask))
		volume, weight := 0, 0
		for i, item := range items {
			if mask&(1<<i) != 0 {
				selected = append(selected, item)
				volume += itemVolume(item)
				weight += item.Weight()
			}
		}
		for _, box := range boxes {
			if budget == 0 || screened >= nodeLimit {
				break searchGeneration
			}
			screened++
			if supply[box] <= 0 || volume > boxInnerVolume(box) || weight > box.MaxWeight()-box.EmptyWeight() {
				continue
			}
			budget--
			if p.packingSearchObserver != nil {
				p.packingSearchObserver()
			}
			vp := NewVolumePacker(box, selected)
			vp.SetMaxConcurrency(p.maxConcurrency)
			add(vp.Pack(), mask)
		}
	}
	sort.SliceStable(patterns, func(i, j int) bool {
		a, b := bits.OnesCount32(patterns[i].mask), bits.OnesCount32(patterns[j].mask)
		if a != b {
			return a > b
		}
		return patterns[i].outerVolume < patterns[j].outerVolume
	})
	byItem := make([][]int, len(items))
	for i, pattern := range patterns {
		for j := range items {
			if pattern.mask&(1<<j) != 0 {
				byItem[j] = append(byItem[j], i)
			}
		}
	}
	best := incumbent
	var path []*PackedBox
	used := make(map[Box]int)
	nodes := 0
	var visit func(uint32, int)
	visit = func(covered uint32, volume int) {
		if nodes >= nodeLimit {
			return
		}
		nodes++
		if covered == full {
			if len(path) < len(best) || (len(path) == len(best) && volume < bestVolume) {
				best = append([]*PackedBox(nil), path...)
				bestVolume = volume
			}
			return
		}
		if len(path) >= len(best) {
			return
		}
		first := bits.TrailingZeros32(full &^ covered)
		for _, index := range byItem[first] {
			pattern := patterns[index]
			if pattern.mask&covered != 0 || used[pattern.box.Box] >= supply[pattern.box.Box] {
				continue
			}
			used[pattern.box.Box]++
			path = append(path, pattern.box)
			visit(covered|pattern.mask, volume+pattern.outerVolume)
			path = path[:len(path)-1]
			used[pattern.box.Box]--
			if nodes >= nodeLimit {
				return
			}
		}
	}
	visit(0, 0)
	// Supply changes are committed only for a complete winning solution.
	for _, box := range incumbent {
		p.boxQuantities[box.Box]++
	}
	for _, box := range best {
		p.boxQuantities[box.Box]--
	}
	return best
}
