package boxpacker

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
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
	ctx                     context.Context
	items                   *itemList
	boxes                   []Box
	boxQuantities           map[Box]int
	allowPartialResults     bool
	quantityShortCircuit    bool
	maxBoxesToBalanceWeight int
	boxSorter               PackedBoxSorter
	boxEvaluationObserver   func(Box)
	maxConcurrency          int
	packingSearchBudget     int
	packingSearchObserver   func()
	boxesSorted             bool
	schedulerObserver       evaluationSchedulerObserver
}

// NewPacker creates an empty Packer. The quantity short-circuit optimisation
// is disabled by default and can be enabled with SetQuantityShortCircuit.
func NewPacker() *Packer {
	return &Packer{
		items:                   &itemList{},
		boxQuantities:           map[Box]int{},
		maxBoxesToBalanceWeight: 12,
		boxSorter:               defaultPackedBoxSorter{},
	}
}

// SetPackedBoxSorter replaces the strategy used to choose the best box at each
// packing iteration, letting callers optimise for a custom objective such as
// minimising billable shipping weight (see BillableWeight). Passing nil
// restores the default ordering (most items, then fullest).
//
// When the quantity short-circuit is enabled, a custom sorter continues to use
// safe per-box item capping, but solved-box replication is disabled because a
// custom comparison can make the winner depend on candidate evaluation order.
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
	p.boxesSorted = false
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
// optimisation. It is disabled by default. When enabled, each box evaluation
// considers a bounded, lookahead-safe number of each item type. With the exact
// built-in packed-box sorter, a solved box can also be replicated while the
// bounded inputs and candidate-box preference partition remain unchanged.
// Custom sorters receive bounded evaluations but never box replication.
func (p *Packer) SetQuantityShortCircuit(enabled bool) {
	p.quantityShortCircuit = enabled
}

// SetMaxConcurrency sets the maximum number of box or orientation evaluations
// this packer may execute concurrently. Zero selects adaptive behavior, one
// forces serial evaluation, and values greater than one are hard ceilings
// rather than target worker counts. Negative values restore adaptive behavior.
// Serial evaluations still participate in the process-wide runtime budget and
// may wait briefly when other pack calls have leased every available slot.
func (p *Packer) SetMaxConcurrency(maxConcurrency int) {
	if maxConcurrency < 0 {
		maxConcurrency = 0
	}
	p.maxConcurrency = maxConcurrency
}

// MaxBoxesToBalanceWeight returns the largest result on which post-pack weight
// redistribution is attempted. The default is 12 boxes.
func (p *Packer) MaxBoxesToBalanceWeight() int {
	return p.maxBoxesToBalanceWeight
}

// SetMaxBoxesToBalanceWeight sets the largest result on which post-pack weight
// redistribution is attempted. Set zero to disable redistribution.
func (p *Packer) SetMaxBoxesToBalanceWeight(maxBoxes int) {
	p.maxBoxesToBalanceWeight = maxBoxes
}

// UnpackedItems returns the items that have not (yet) been packed.
func (p *Packer) UnpackedItems() []Item {
	return p.items.toSlice()
}

// Pack packs the items into boxes and returns the packed boxes.
func (p *Packer) Pack() ([]*PackedBox, error) {
	return p.PackContext(context.Background())
}

// PackContext packs with cooperative cancellation. Cancellation returns ctx.Err()
// even when partial results are allowed. Only completed greedy boxes are committed;
// an interrupted candidate is never accepted. Packer is not safe for concurrent use.
// Custom Item, Box and sorter methods must return promptly; their calls cannot be
// forcibly interrupted by a context.
func (p *Packer) PackContext(ctx context.Context) ([]*PackedBox, error) {
	if ctx == nil {
		panic("boxpacker: nil context")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	previous := p.ctx
	p.ctx = ctx
	defer func() { p.ctx = previous }()
	var searchItems *itemList
	var searchSupply map[Box]int
	if p.packingSearchBudget > 0 && p.items.count() <= maximumPackingSearchItems {
		if _, builtin := p.boxSorter.(defaultPackedBoxSorter); builtin {
			searchItems = p.items.clone()
			searchSupply = make(map[Box]int, len(p.boxQuantities))
			for box, quantity := range p.boxQuantities {
				searchSupply[box] = quantity
			}
		}
	}
	packedBoxes, err := p.packBasic(false)
	if err == nil && p.items.count() == 0 && searchItems != nil && len(packedBoxes) > 1 {
		packedBoxes = p.searchPacking(searchItems, searchSupply, packedBoxes)
	}
	if err == nil && contextError(ctx) == nil && len(packedBoxes) > 1 && len(packedBoxes) <= p.maxBoxesToBalanceWeight {
		redistributor := newWeightRedistributor(p.boxes, p.boxSorter, p.boxQuantities, p.maxConcurrency)
		redistributor.ctx = ctx
		packedBoxes = redistributor.redistributeWeight(packedBoxes)
	}
	if cancelErr := contextError(ctx); cancelErr != nil {
		return packedBoxes, cancelErr
	}
	// PHP exposes a PackedBoxList that sorts lazily on iteration. Go returns a
	// slice, so apply the active sorter before returning every result, including
	// partial boxes returned alongside NoBoxesAvailableError and results for
	// which weight redistribution is disabled or skipped.
	sort.SliceStable(packedBoxes, func(i, j int) bool {
		return p.boxSorter.Compare(packedBoxes[i], packedBoxes[j]) < 0
	})
	if cancelErr := contextError(ctx); cancelErr != nil {
		return packedBoxes, cancelErr
	}
	return packedBoxes, err
}

// packBasic performs the greedy packing pass without post-pack weight
// redistribution. When enforceSingleBox is true, boxes that cannot hold the
// entire remaining item volume are not candidates and an unpackable tail is
// returned without error. Weight redistribution uses this mode to test whether
// a proposed item set can still be packed into one box.
func (p *Packer) packBasic(enforceSingleBox bool) ([]*PackedBox, error) {
	if p.boxSorter == nil {
		p.boxSorter = defaultPackedBoxSorter{}
	}

	var packedBoxes []*PackedBox

	// Keep going until everything is packed
	for p.items.count() > 0 {
		if err := contextError(p.ctx); err != nil {
			return packedBoxes, err
		}
		// Evaluate independent candidates within one shared concurrency budget.
		// With a single candidate, spare capacity may instead be used for that
		// box's first-orientation alternatives. Multiple candidates always use
		// serial volume packers so scheduler pools are never nested.
		candidates := p.candidateBoxes(enforceSingleBox)
		p.items.ensureSorted() // so the per-candidate clones don't each re-sort
		var signatureCounts map[itemSignature]int
		if p.quantityShortCircuit {
			signatureCounts = p.items.signatureCounts()
		}
		packers := make([]*volumePacker, len(candidates))
		for i, box := range candidates {
			if err := contextError(p.ctx); err != nil {
				return packedBoxes, err
			}
			packers[i] = newVolumePackerWithContext(p.ctx, box, p.itemsForBoxEvaluation(box, signatureCounts))
		}
		results := p.evaluateCandidates(packers)
		if err := contextError(p.ctx); err != nil {
			return packedBoxes, err
		}

		var iteration []*PackedBox
		for _, packedBox := range results {
			if packedBox != nil && len(packedBox.Items) > 0 {
				iteration = append(iteration, packedBox)
			}
		}

		if len(iteration) == 0 {
			if p.allowPartialResults || enforceSingleBox {
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

	return packedBoxes, contextError(p.ctx)
}

func (p *Packer) evaluateCandidates(packers []*volumePacker) []*PackedBox {
	results := make([]*PackedBox, len(packers))
	if len(packers) == 0 {
		return results
	}

	observeBox := func(packer *volumePacker) {
		if p.boxEvaluationObserver != nil {
			p.boxEvaluationObserver(packer.box)
		}
	}

	// A single candidate has no box-level parallelism to exploit, so let its
	// volume packer adaptively use the same budget for first orientations.
	if len(packers) == 1 {
		observeBox(packers[0])
		results[0] = packers[0].packWithConcurrency(p.maxConcurrency, p.schedulerObserver)
		return results
	}

	estimatedWork := 0
	for _, packer := range packers {
		estimatedWork += packer.estimatedWork()
	}
	desiredWorkers := candidateEvaluationWorkers(p.maxConcurrency, len(packers), estimatedWork)
	workers, err := sharedEvaluationWorkers.acquireContext(p.ctx, desiredWorkers)
	if err != nil {
		return results
	}
	defer sharedEvaluationWorkers.release(workers)
	if workers <= 1 {
		for index, packer := range packers {
			if contextError(p.ctx) != nil {
				break
			}
			observeBox(packer)
			if p.schedulerObserver != nil {
				p.schedulerObserver(evaluationCandidate, 1)
			}
			results[index] = packer.packSerial()
		}
		return results
	}

	jobs := make(chan int, workers)
	var waitGroup sync.WaitGroup
	var active atomic.Int64
	waitGroup.Add(workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			for index := range jobs {
				if contextError(p.ctx) != nil {
					continue
				}
				packer := packers[index]
				observeBox(packer)
				current := int(active.Add(1))
				if p.schedulerObserver != nil {
					p.schedulerObserver(evaluationCandidate, current)
				}
				results[index] = packer.packSerial()
				active.Add(-1)
			}
		}()
	}
	for index := range packers {
		if contextError(p.ctx) != nil {
			break
		}
		// Workers always drain the bounded jobs channel, then are joined below.
		jobs <- index
	}
	close(jobs)
	waitGroup.Wait()
	return results
}

// replicateIdenticalBoxes is the large-quantity short-circuit. The box just
// packed is the winner of a full evaluation of the current bounded pool. It can
// be copied only while every iteration it replaces would retain that same
// bounded pool for every item signature in the template. The bound is the
// largest physical capacity of any currently available box plus the orientation
// lookahead window.
//
// This handles a winning box made up of a mix of different item types, not just
// a single type: the multiset of item signatures is what gets replicated.
//
// Once any component drops below that floor, placement or box selection may
// change as the pool depletes, so the shrinking tail goes through normal
// evaluation.
func (p *Packer) replicateIdenticalBoxes(template *PackedBox) []*PackedBox {
	// Only the exact built-in sorter has the ordering properties used by this
	// proof. In particular, a custom sorter may tie candidates whose order then
	// changes as the preferred-box partition changes.
	if _, ok := p.boxSorter.(defaultPackedBoxSorter); !ok {
		return nil
	}

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
	// Stable sorter ties between physically different signatures can interleave
	// their runs. Counts alone cannot prove that replica inputs stay unchanged.
	for sig := range boxCounts {
		if hasSortTiedSignature(sig, poolCounts) {
			return nil
		}
	}
	replications := p.boxQuantities[template.Box]

	// Candidate boxes are partitioned before each iteration: boxes large enough
	// to hold the whole remaining item volume are evaluated first. Replication
	// must stop before a previously non-preferred box crosses that boundary,
	// because the new evaluation order can resolve a sorter tie differently.
	poolVolume := p.items.totalVolume()
	templateVolume := template.UsedVolume()
	templateIterationVolume := poolVolume + templateVolume
	largestNonPreferredBoxVolume := 0
	hasNonPreferredBox := false
	for _, candidate := range p.boxes {
		if p.boxQuantities[candidate] <= 0 {
			continue
		}
		candidateVolume := boxInnerVolume(candidate)
		if candidateVolume < templateIterationVolume {
			largestNonPreferredBoxVolume = maxInt(largestNonPreferredBoxVolume, candidateVolume)
			hasNonPreferredBox = true
		}
	}
	if hasNonPreferredBox {
		if poolVolume <= largestNonPreferredBoxVolume {
			return nil
		}
		if templateVolume > 0 {
			possible := (poolVolume-largestNonPreferredBoxVolume-1)/templateVolume + 1
			if possible < replications {
				replications = possible
			}
		}
	}

	// A prospective replica replaces an iteration whose pre-pack pool must still
	// contain the full bounded evaluation window for every component signature.
	// Use the maximum capacity across all box types that remain in stock because
	// every one of them would be evaluated in that iteration.
	for sig, need := range boxCounts {
		have := poolCounts[sig]
		maxCapacity := 0
		for _, candidate := range p.boxes {
			if p.boxQuantities[candidate] > 0 {
				maxCapacity = maxInt(maxCapacity, perBoxCapacity(candidate, sig))
			}
		}

		mustRemain := maxCapacity + orientationLookaheadDepth
		if have < mustRemain {
			return nil
		}
		// Largest k for which the kth replaced iteration begins with at least
		// mustRemain copies: have-(k-1)*need >= mustRemain.
		if k := (have-mustRemain)/need + 1; k < replications {
			replications = k
		}
	}
	if replications <= 0 {
		return nil
	}

	toRemove := make(map[itemSignature]int, len(boxCounts))
	for sig, need := range boxCounts {
		toRemove[sig] = need * replications
	}
	workingItems := p.items.clone()
	taken := workingItems.takeSignatureMultiset(toRemove)
	clones := make([]*PackedBox, replications)
	for i := range clones {
		if contextError(p.ctx) != nil {
			return nil
		}
		clones[i] = template.clone()
		for index, placement := range clones[i].Items {
			if index%128 == 0 && contextError(p.ctx) != nil {
				return nil
			}
			sig := signatureOf(placement.Item)
			placement.Item = taken[sig].extract()
		}
	}
	if contextError(p.ctx) != nil {
		return nil
	}
	p.items = workingItems
	p.boxQuantities[template.Box] -= replications
	return clones
}

// itemsForBoxEvaluation bounds the work done per box evaluation. A box can never
// hold more items of a given type than its volume and weight allow, but the
// orientation sorter also looks ahead at upcoming items. Retaining physical
// capacity plus orientationLookaheadDepth copies per signature ensures that
// capped and uncapped evaluations see the same lookahead window throughout the
// pack while keeping work independent of total order quantity. signatureCounts
// is the pool's precomputed signature->count map, shared across all candidate
// boxes in this iteration.
func (p *Packer) itemsForBoxEvaluation(box Box, signatureCounts map[itemSignature]int) *itemList {
	if !p.quantityShortCircuit || len(signatureCounts) == 0 {
		return p.items
	}

	caps := make(map[itemSignature]int, len(signatureCounts))
	needsCap := false
	for sig, have := range signatureCounts {
		cap := perBoxCapacity(box, sig) + orientationLookaheadDepth
		caps[sig] = cap
		if cap < have {
			needsCap = true
		}
	}
	if !needsCap {
		return p.items
	}
	return p.items.cappedBySignature(caps)
}

// perBoxCapacity is an upper bound on how many copies of an item signature a
// box could hold by volume and weight. Geometry may lower the real capacity,
// but can never raise it.
func perBoxCapacity(box Box, sig itemSignature) int {
	unitVolume := maxInt(sig.width*sig.length*sig.depth, 1)
	capacity := boxInnerVolume(box) / unitVolume
	if sig.weight > 0 {
		capacity = minInt(capacity, (box.MaxWeight()-box.EmptyWeight())/sig.weight)
	}
	return maxInt(capacity, 0)
}

// candidateBoxes returns a "smart" ordering of the boxes to try packing items
// into: smallest first, but boxes that cannot possibly hold the entire
// remaining set of items by volume are evaluated last.
func (p *Packer) candidateBoxes(enforceSingleBox bool) []Box {
	if !p.boxesSorted {
		sort.SliceStable(p.boxes, func(i, j int) bool { return compareBoxes(p.boxes[i], p.boxes[j]) < 0 })
		p.boxesSorted = true
	}
	sorted := p.boxes

	remainingVolume := p.items.totalVolume()

	var preferred, other []Box
	for _, box := range sorted {
		if p.boxQuantities[box] <= 0 {
			continue
		}
		if boxInnerVolume(box) >= remainingVolume {
			preferred = append(preferred, box)
		} else if !enforceSingleBox {
			other = append(other, box)
		}
	}
	return append(preferred, other...)
}
