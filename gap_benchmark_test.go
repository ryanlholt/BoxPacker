package boxpacker_test

// This file is a benchmark/analysis harness (not a pass/fail unit test) that
// measures the gap between the library's greedy packer and a near-optimal
// pattern-based solver across several realistic catalogs.
//
// Run with:  go test -run GreedyOptimalGap -v
//
// Methodology
// -----------
// Optimal 3D bin packing is NP-hard, so we bound the gap from two sides:
//
//  1. Lower bound (LB) on the optimal box count: the max of a volume bound, a
//     weight bound, and a max-items-per-box bound. No packing can use fewer
//     boxes than LB, so (greedy-LB)/LB is an UPPER bound on greedy's gap. If
//     greedy == LB, greedy is provably optimal.
//
//  2. An alternative pattern-based solver (ALT). Every candidate "pattern" (a
//     multiset of SKUs assigned to a box) is validated by the very same
//     VolumePacker the greedy uses, so any covering it produces is physically
//     achievable - a partially filled final box only ever holds FEWER items
//     than a validated pattern, which always fits. Therefore if ALT uses fewer
//     boxes than greedy, that is a CONCRETE, PROVEN suboptimality of greedy.
//     On small instances ALT exhaustively searches the generated pattern pool,
//     which is not a complete enumeration of feasible geometric packings. On
//     large-quantity instances it runs a fast bulk greedy
//     set-cover, giving a valid feasible (upper-bound-on-optimal) solution.
//
// We report box count (primary objective) and total outer/shipping volume.

import (
	"fmt"
	"sort"
	"testing"
	"time"

	boxpacker "github.com/ryanlholt/BoxPacker"
)

// ---- catalog definitions -------------------------------------------------

type benchSKU struct {
	name            string
	w, l, d, weight int
	rot             boxpacker.Rotation
	qty             int
}

type benchBox struct {
	ref              string
	ow, ol, od, ew   int
	iw, il, id, maxw int
}

type benchCatalog struct {
	name  string
	boxes []benchBox
	skus  []benchSKU
}

func (c benchCatalog) boxObjects() []*boxpacker.StandardBox {
	out := make([]*boxpacker.StandardBox, len(c.boxes))
	for i, b := range c.boxes {
		out[i] = boxpacker.NewBox(b.ref, b.ow, b.ol, b.od, b.ew, b.iw, b.il, b.id, b.maxw)
	}
	return out
}

func itemsFor(s benchSKU, n int) []boxpacker.Item {
	out := make([]boxpacker.Item, n)
	for i := range out {
		out[i] = boxpacker.NewItem(s.name, s.w, s.l, s.d, s.weight, s.rot)
	}
	return out
}

// realistic-ish catalogs (dimensions in mm, weight in g)
func benchCatalogs() []benchCatalog {
	return []benchCatalog{
		{
			name: "single-SKU bulk (paperback books, x500)",
			boxes: []benchBox{
				{"book-mailer", 230, 160, 60, 80, 225, 155, 55, 2000},
				{"small-carton", 320, 240, 130, 200, 310, 230, 120, 8000},
				{"medium-carton", 400, 300, 200, 350, 388, 290, 190, 15000},
			},
			skus: []benchSKU{
				{"paperback", 195, 130, 25, 250, boxpacker.RotationKeepFlat, 500},
			},
		},
		{
			name: "few-SKU mixed apparel (x~300 each)",
			boxes: []benchBox{
				{"poly-small", 250, 200, 100, 60, 244, 194, 94, 5000},
				{"carton-med", 400, 300, 250, 300, 388, 290, 240, 12000},
				{"carton-large", 500, 400, 300, 450, 488, 388, 290, 20000},
			},
			skus: []benchSKU{
				{"tshirt", 200, 150, 40, 200, boxpacker.RotationBestFit, 300},
				{"jeans", 300, 200, 60, 600, boxpacker.RotationBestFit, 250},
				{"jacket", 350, 250, 90, 900, boxpacker.RotationBestFit, 150},
			},
		},
		{
			name: "large mixed quantity (3 SKUs x4000)",
			boxes: []benchBox{
				{"cube-s", 160, 160, 160, 120, 150, 150, 150, 8000},
				{"cube-l", 320, 320, 320, 400, 310, 310, 310, 30000},
			},
			skus: []benchSKU{
				{"A", 50, 50, 50, 50, boxpacker.RotationBestFit, 4000},
				{"B", 40, 40, 40, 40, boxpacker.RotationBestFit, 4000},
				{"C", 30, 30, 30, 30, boxpacker.RotationBestFit, 4000},
			},
		},
		{
			name: "small order, 5 SKUs low qty",
			boxes: []benchBox{
				{"s", 220, 180, 120, 80, 214, 174, 114, 4000},
				{"m", 320, 260, 180, 200, 312, 252, 172, 10000},
				{"l", 420, 340, 240, 350, 410, 330, 230, 18000},
			},
			skus: []benchSKU{
				{"mug", 100, 100, 110, 350, boxpacker.RotationNever, 4},
				{"book", 200, 130, 30, 500, boxpacker.RotationKeepFlat, 6},
				{"cable", 120, 80, 40, 120, boxpacker.RotationBestFit, 5},
				{"bottle", 80, 80, 200, 700, boxpacker.RotationKeepFlat, 3},
				{"box-game", 270, 270, 50, 600, boxpacker.RotationKeepFlat, 2},
			},
		},
		{
			name: "awkward poorly-tiling sizes (x80 each)",
			boxes: []benchBox{
				{"a", 300, 300, 300, 200, 290, 290, 290, 15000},
				{"b", 400, 300, 300, 300, 388, 290, 290, 18000},
			},
			skus: []benchSKU{
				// 130 doesn't tile cleanly into 290 (2 fit, 30 wasted each axis)
				{"chunk", 130, 130, 130, 800, boxpacker.RotationBestFit, 80},
				{"slab", 280, 120, 70, 500, boxpacker.RotationBestFit, 80},
			},
		},

		// Small twins of the catalogs above permit exhaustive pattern-pool search,
		// without claiming complete geometric enumeration.
		{
			name: "[small] apparel twin (6/5/3)",
			boxes: []benchBox{
				{"poly-small", 250, 200, 100, 60, 244, 194, 94, 5000},
				{"carton-med", 400, 300, 250, 300, 388, 290, 240, 12000},
				{"carton-large", 500, 400, 300, 450, 488, 388, 290, 20000},
			},
			skus: []benchSKU{
				{"tshirt", 200, 150, 40, 200, boxpacker.RotationBestFit, 6},
				{"jeans", 300, 200, 60, 600, boxpacker.RotationBestFit, 5},
				{"jacket", 350, 250, 90, 900, boxpacker.RotationBestFit, 3},
			},
		},
		{
			name: "[small] cube twin (A/B/C x8)",
			boxes: []benchBox{
				{"cube-s", 160, 160, 160, 120, 150, 150, 150, 8000},
				{"cube-l", 320, 320, 320, 400, 310, 310, 310, 30000},
			},
			skus: []benchSKU{
				{"A", 50, 50, 50, 50, boxpacker.RotationBestFit, 8},
				{"B", 40, 40, 40, 40, boxpacker.RotationBestFit, 8},
				{"C", 30, 30, 30, 30, boxpacker.RotationBestFit, 8},
			},
		},
		{
			name: "[small] awkward twin (x10 each)",
			boxes: []benchBox{
				{"a", 300, 300, 300, 200, 290, 290, 290, 15000},
				{"b", 400, 300, 300, 300, 388, 290, 290, 18000},
			},
			skus: []benchSKU{
				{"chunk", 130, 130, 130, 800, boxpacker.RotationBestFit, 10},
				{"slab", 280, 120, 70, 500, boxpacker.RotationBestFit, 10},
			},
		},
	}
}

// ---- lower bounds --------------------------------------------------------

func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// lowerBound on the number of boxes needed for the given remaining demand.
func lowerBound(c benchCatalog, demand []int) int {
	totalVol, totalWeight, totalItems := 0, 0, 0
	minVol, minWeight := 1<<62, 1<<62
	for i, s := range c.skus {
		if demand[i] <= 0 {
			continue
		}
		v := s.w * s.l * s.d
		totalVol += v * demand[i]
		totalWeight += s.weight * demand[i]
		totalItems += demand[i]
		if v < minVol {
			minVol = v
		}
		if s.weight < minWeight {
			minWeight = s.weight
		}
	}
	if totalItems == 0 {
		return 0
	}

	maxInnerVol, maxNet, maxItemsPerBox := 0, 0, 0
	for _, b := range c.boxes {
		inner := b.iw * b.il * b.id
		net := b.maxw - b.ew
		if inner > maxInnerVol {
			maxInnerVol = inner
		}
		if net > maxNet {
			maxNet = net
		}
		cap := inner / minVol
		if minWeight > 0 {
			if wc := net / minWeight; wc < cap {
				cap = wc
			}
		}
		if cap > maxItemsPerBox {
			maxItemsPerBox = cap
		}
	}

	lb := ceilDiv(totalVol, maxInnerVol)
	if wb := ceilDiv(totalWeight, maxNet); wb > lb {
		lb = wb
	}
	if ib := ceilDiv(totalItems, maxItemsPerBox); ib > lb {
		lb = ib
	}
	return lb
}

// ---- pattern generation (geometry-validated) -----------------------------

type pattern struct {
	boxIdx   int
	counts   []int // per SKU
	outerVol int
	items    int
}

// fitInto packs the given per-SKU item counts into box b using the real
// VolumePacker and returns how many of each SKU actually fit.
func fitInto(c benchCatalog, boxObj boxpacker.Box, want []int) []int {
	var items []boxpacker.Item
	for i, s := range c.skus {
		if want[i] > 0 {
			items = append(items, itemsFor(s, want[i])...)
		}
	}
	if len(items) == 0 {
		return make([]int, len(c.skus))
	}
	packed := boxpacker.NewVolumePacker(boxObj, items).Pack()
	got := make([]int, len(c.skus))
	idx := map[string]int{}
	for i, s := range c.skus {
		idx[s.name] = i
	}
	for _, pi := range packed.Items {
		got[idx[pi.Item.Description()]]++
	}
	return got
}

// generatePatterns builds a diverse, geometry-validated set of candidate
// patterns. Inputs are capped at the available demand per SKU so every pattern
// is individually realizable.
func generatePatterns(c benchCatalog, demand []int) []pattern {
	boxes := c.boxObjects()
	seen := map[string]bool{}
	var pats []pattern

	add := func(boxIdx int, counts []int) {
		total := 0
		for _, n := range counts {
			total += n
		}
		if total == 0 {
			return
		}
		key := fmt.Sprintf("%d:%v", boxIdx, counts)
		if seen[key] {
			return
		}
		seen[key] = true
		b := c.boxes[boxIdx]
		pats = append(pats, pattern{
			boxIdx:   boxIdx,
			counts:   counts,
			outerVol: b.ow * b.ol * b.od,
			items:    total,
		})
	}

	volCap := func(boxIdx, sku int) int {
		b := c.boxes[boxIdx]
		s := c.skus[sku]
		cap := (b.iw * b.il * b.id) / (s.w * s.l * s.d)
		if s.weight > 0 {
			if wc := (b.maxw - b.ew) / s.weight; wc < cap {
				cap = wc
			}
		}
		if cap > demand[sku] {
			cap = demand[sku]
		}
		return cap
	}

	for bi := range boxes {
		// single-SKU max-fill patterns
		for si := range c.skus {
			want := make([]int, len(c.skus))
			want[si] = volCap(bi, si)
			if want[si] <= 0 {
				continue
			}
			add(bi, fitInto(c, boxes[bi], want))
		}

		// mixed patterns under several SKU priority orderings: each ordering
		// fills greedily by that priority, capped at demand, then the geometry
		// engine decides what actually fits together.
		orders := skuOrderings(c)
		for _, order := range orders {
			want := make([]int, len(c.skus))
			b := c.boxes[bi]
			volumeLeft, weightLeft := b.iw*b.il*b.id, b.maxw-b.ew
			for _, si := range order {
				s := c.skus[si]
				quantity := volCap(bi, si)
				if q := volumeLeft / (s.w * s.l * s.d); q < quantity {
					quantity = q
				}
				if s.weight > 0 {
					if q := weightLeft / s.weight; q < quantity {
						quantity = q
					}
				}
				want[si] = quantity
				volumeLeft -= quantity * s.w * s.l * s.d
				weightLeft -= quantity * s.weight
			}
			add(bi, fitInto(c, boxes[bi], want))
		}
	}
	return pats
}

func skuOrderings(c benchCatalog) [][]int {
	n := len(c.skus)
	base := make([]int, n)
	for i := range base {
		base[i] = i
	}
	byVol := append([]int(nil), base...)
	sort.SliceStable(byVol, func(i, j int) bool {
		a, b := c.skus[byVol[i]], c.skus[byVol[j]]
		return a.w*a.l*a.d > b.w*b.l*b.d
	})
	byVolAsc := append([]int(nil), byVol...)
	for i, j := 0, len(byVolAsc)-1; i < j; i, j = i+1, j-1 {
		byVolAsc[i], byVolAsc[j] = byVolAsc[j], byVolAsc[i]
	}
	byWeight := append([]int(nil), base...)
	sort.SliceStable(byWeight, func(i, j int) bool {
		return c.skus[byWeight[i]].weight > c.skus[byWeight[j]].weight
	})
	return [][]int{base, byVol, byVolAsc, byWeight}
}

// ---- alternative solver: bulk greedy set-cover + small-instance B&B ------

type altResult struct {
	boxes                 int
	outerVol              int
	patternSearchComplete bool // exhausted the restricted pattern search, not a proof of global optimality
}

func sub(demand, counts []int, k int) []int {
	out := make([]int, len(demand))
	for i := range demand {
		out[i] = demand[i] - counts[i]*k
		if out[i] < 0 {
			out[i] = 0
		}
	}
	return out
}

func covered(demand []int) bool {
	for _, d := range demand {
		if d > 0 {
			return false
		}
	}
	return true
}

// bulkGreedyCover repeatedly applies the pattern covering the most remaining
// demand, in bulk, until all demand is met. Always feasible.
func bulkGreedyCover(c benchCatalog, pats []pattern, demand []int) (int, int) {
	rem := append([]int(nil), demand...)
	boxes, outerVol := 0, 0
	for !covered(rem) {
		bestIdx, bestScore, bestWaste := -1, -1, 1<<62
		for pi, p := range pats {
			score := 0
			for i := range rem {
				c := p.counts[i]
				if c > rem[i] {
					c = rem[i]
				}
				score += c
			}
			if score == 0 {
				continue
			}
			// waste = outer volume spent per unit of remaining demand covered
			waste := p.outerVol / score
			if score > bestScore || (score == bestScore && waste < bestWaste) {
				bestIdx, bestScore, bestWaste = pi, score, waste
			}
		}
		if bestIdx < 0 {
			return 1 << 30, 1 << 30 // infeasible (shouldn't happen)
		}
		p := pats[bestIdx]
		// bulk factor: how many full copies we can still use
		k := 1 << 30
		for i := range rem {
			if p.counts[i] > 0 {
				if q := rem[i] / p.counts[i]; q < k {
					k = q
				}
			}
		}
		if k < 1 {
			k = 1
		}
		boxes += k
		outerVol += p.outerVol * k
		rem = sub(rem, p.counts, k)
	}
	return boxes, outerVol
}

// branchAndBound searches only the supplied patterns for small instances.
// Completion proves a minimum count within this pool, not the global optimum.
// The boolean is false if the node budget was exhausted.
func branchAndBound(c benchCatalog, pats []pattern, demand []int, seedBoxes, seedVol int) (int, int, bool) {
	bestBoxes, bestVol := seedBoxes, seedVol

	const nodeBudget = 4_000_000
	nodes := 0
	exhausted := true

	// Complete enumeration of pattern multisets via non-decreasing index order:
	// recursing with start=pi (not pi+1) allows repeats, so every multiset is
	// reached exactly once in its canonical ascending ordering. The ONLY pruning
	// is the admissible lower bound, so completion proves the best box count
	// within this restricted pattern pool. A pattern covering nothing still
	// needed is skipped because it cannot improve a minimum-count solution.
	contributes := func(rem []int, p pattern) bool {
		for i, d := range rem {
			if d > 0 && p.counts[i] > 0 {
				return true
			}
		}
		return false
	}
	var dfs func(rem []int, start, usedBoxes, usedVol int)
	dfs = func(rem []int, start, usedBoxes, usedVol int) {
		if !exhausted {
			return
		}
		if covered(rem) {
			if usedBoxes < bestBoxes || (usedBoxes == bestBoxes && usedVol < bestVol) {
				bestBoxes, bestVol = usedBoxes, usedVol
			}
			return
		}
		if usedBoxes+lowerBound(c, rem) >= bestBoxes {
			return // prune: cannot beat incumbent (admissible bound)
		}
		nodes++
		if nodes > nodeBudget {
			exhausted = false
			return
		}
		for pi := start; pi < len(pats); pi++ {
			p := pats[pi]
			if !contributes(rem, p) {
				continue
			}
			dfs(sub(rem, p.counts, 1), pi, usedBoxes+1, usedVol+p.outerVol)
		}
	}

	dfs(demand, 0, 0, 0)
	return bestBoxes, bestVol, exhausted
}

// greedyPatterns extracts the per-box layouts the library greedy actually
// produced, so the alternative solver's pool can always at least reconstruct
// greedy's solution. This guarantees ALT is never worse than greedy.
func greedyPatterns(c benchCatalog, packed []*boxpacker.PackedBox) []pattern {
	boxIdx := map[string]int{}
	for i, b := range c.boxes {
		boxIdx[b.ref] = i
	}
	skuIdx := map[string]int{}
	for i, s := range c.skus {
		skuIdx[s.name] = i
	}
	seen := map[string]bool{}
	var pats []pattern
	for _, pb := range packed {
		bi := boxIdx[pb.Box.Reference()]
		counts := make([]int, len(c.skus))
		for _, pi := range pb.Items {
			counts[skuIdx[pi.Item.Description()]]++
		}
		key := fmt.Sprintf("%d:%v", bi, counts)
		if seen[key] {
			continue
		}
		seen[key] = true
		b := c.boxes[bi]
		total := 0
		for _, n := range counts {
			total += n
		}
		pats = append(pats, pattern{boxIdx: bi, counts: counts, outerVol: b.ow * b.ol * b.od, items: total})
	}
	return pats
}

// solveAlt returns the best alternative solution found. greedyBoxes/greedyVol
// seed the incumbent so ALT can only match or beat greedy; any improvement is
// therefore a concrete, achievable gap.
func solveAlt(c benchCatalog, demand []int, packed []*boxpacker.PackedBox, greedyBoxes, greedyVol int) altResult {
	pats := append(generatePatterns(c, demand), greedyPatterns(c, packed)...)

	// candidate 1: greedy itself
	best := altResult{boxes: greedyBoxes, outerVol: greedyVol, patternSearchComplete: false}

	// candidate 2: bulk greedy set-cover over the full pool
	if b, v := bulkGreedyCover(c, pats, demand); b < best.boxes || (b == best.boxes && v < best.outerVol) {
		best.boxes, best.outerVol = b, v
	}

	// candidate 3: exhaustive restricted-pattern search for small instances
	total := 0
	for _, d := range demand {
		total += d
	}
	if total <= 80 {
		b, v, complete := branchAndBound(c, pats, demand, best.boxes, best.outerVol)
		if b < best.boxes || (b == best.boxes && v < best.outerVol) {
			best.boxes, best.outerVol = b, v
		}
		best.patternSearchComplete = complete
	}
	return best
}

// ---- the benchmark -------------------------------------------------------

func TestGreedyOptimalGap(t *testing.T) {
	fmt.Printf("\n%-44s %8s %8s %8s %7s  %s\n",
		"catalog", "greedy", "LB", "alt", "gap", "notes")
	fmt.Println("--------------------------------------------------------------------------------------------")

	for _, c := range benchCatalogs() {
		demand := make([]int, len(c.skus))
		for i, s := range c.skus {
			demand[i] = s.qty
		}

		// library greedy
		p := boxpacker.NewPacker()
		for _, b := range c.boxObjects() {
			p.AddBox(b)
		}
		for _, s := range c.skus {
			p.AddItem(boxpacker.NewItem(s.name, s.w, s.l, s.d, s.weight, s.rot), s.qty)
		}
		start := time.Now()
		packed, err := p.Pack()
		greedyDur := time.Since(start)
		if err != nil {
			t.Fatalf("%s: greedy pack failed: %v", c.name, err)
		}
		greedyBoxes := len(packed)
		greedyVol := 0
		for _, pb := range packed {
			greedyVol += pb.Box.OuterWidth() * pb.Box.OuterLength() * pb.Box.OuterDepth()
		}

		lb := lowerBound(c, demand)

		startAlt := time.Now()
		alt := solveAlt(c, demand, packed, greedyBoxes, greedyVol)
		altDur := time.Since(startAlt)

		// gap: prefer proven gap vs alt; report bound vs LB too
		provenGap := float64(greedyBoxes-alt.boxes) / float64(alt.boxes) * 100
		boundGap := float64(greedyBoxes-lb) / float64(lb) * 100

		var note string
		switch {
		case greedyBoxes == lb:
			note = "greedy = OPTIMUM (matches independent LB)"
		case alt.boxes < greedyBoxes:
			note = fmt.Sprintf("ALT beat greedy by %d box(es); global optimum unknown", greedyBoxes-alt.boxes)
		case alt.patternSearchComplete:
			note = "restricted pattern search exhausted; global optimum unknown"
		default:
			note = "no better found; global optimum unknown"
		}

		fmt.Printf("%-44s %8d %8d %8d %6.1f%%  %s\n",
			truncate(c.name, 44), greedyBoxes, lb, alt.boxes, provenGap, note)
		fmt.Printf("%-44s vol(m^3): greedy=%.3f alt=%.3f  boundGap=%.1f%%  t: greedy=%s alt=%s\n",
			"", float64(greedyVol)/1e9, float64(alt.outerVol)/1e9, boundGap,
			greedyDur.Round(time.Microsecond), altDur.Round(time.Microsecond))
	}
	fmt.Println()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
