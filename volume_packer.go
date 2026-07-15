package boxpacker

import (
	"sync"
	"sync/atomic"
)

// VolumePacker packs as many items as possible into a single specific box.
type volumePacker struct {
	box                 Box
	items               *itemList
	layerPacker         *layerPacker
	singlePassMode      bool
	packAcrossWidthOnly bool
	hasNoRotationItems  bool
	maxConcurrency      int
	schedulerObserver   evaluationSchedulerObserver
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

// SetMaxConcurrency sets the maximum number of evaluations this packer may
// execute concurrently. Zero selects adaptive behavior, one forces serial
// evaluation, and values greater than one are hard ceilings rather than target
// worker counts. Negative values restore adaptive behavior.
func (vp *VolumePacker) SetMaxConcurrency(maxConcurrency int) {
	if maxConcurrency < 0 {
		maxConcurrency = 0
	}
	vp.inner.maxConcurrency = maxConcurrency
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
	return vp.packWithConcurrency(vp.maxConcurrency, vp.schedulerObserver)
}

type volumeEvaluationTask struct {
	boxWidth, boxLength int
	firstItem           *orientatedItem
}

type completedVolumeEvaluation struct {
	index int
	box   *PackedBox
}

func (vp *volumePacker) evaluationTasks() []volumeEvaluationTask {
	if vp.items.count() == 0 {
		return nil
	}

	// Sometimes "space available" decisions depend on orientation of the box, so try both ways
	rotationsToTest := []bool{false}
	if !vp.packAcrossWidthOnly && !vp.hasNoRotationItems {
		rotationsToTest = append(rotationsToTest, true)
	}

	// The orientation of the first item can have an outsized effect on the rest
	// of the placement, so special-case it and try every valid orientation.
	var tasks []volumeEvaluationTask
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
			tasks = append(tasks, volumeEvaluationTask{boxWidth: boxWidth, boxLength: boxLength, firstItem: firstItemOrientation})
		}
	}
	return tasks
}

func (vp *volumePacker) runEvaluationTask(task volumeEvaluationTask) *PackedBox {
	return vp.packRotation(task.boxWidth, task.boxLength, task.firstItem)
}

func (vp *volumePacker) packSerial() *PackedBox {
	if vp.items.count() == 0 {
		return newPackedBox(vp.box, &packedItemList{})
	}
	return vp.reduceEvaluationTasks(vp.evaluationTasks(), 1, nil)
}

func (vp *volumePacker) packWithConcurrency(maxConcurrency int, observer evaluationSchedulerObserver) *PackedBox {
	if vp.items.count() == 0 {
		return newPackedBox(vp.box, &packedItemList{})
	}
	tasks := vp.evaluationTasks()
	workers := orientationEvaluationWorkers(maxConcurrency, vp.items.count(), len(tasks), vp.singlePassMode)
	if vp.singlePassMode {
		// Lookahead executes inside its parent's evaluation-worker lease and must
		// not recursively acquire another slot.
		return vp.reduceEvaluationTasks(tasks, 1, observer)
	}
	leasedWorkers := sharedEvaluationWorkers.acquire(1)
	defer func() { sharedEvaluationWorkers.release(leasedWorkers) }()
	if workers <= 1 {
		return vp.reduceEvaluationTasks(tasks, 1, observer)
	}

	// Preserve the serial algorithm's cheapest and most common early exit. If
	// the first orientation fits everything, do not speculatively solve a second
	// orientation and wait for work whose result cannot be selected.
	first := vp.reduceEvaluationTasks(tasks[:1], 1, observer)
	if len(first.Items) == vp.items.count() || len(tasks) == 1 {
		return first
	}

	// Do not queue behind other pack calls merely to widen this pool. The one
	// leased worker can continue serially under saturation; otherwise claim any
	// immediately spare capacity up to the per-instance ceiling.
	remainingWorkerLimit := minInt(workers, len(tasks)-1)
	leasedWorkers += sharedEvaluationWorkers.tryAcquire(remainingWorkerLimit - leasedWorkers)
	remaining := vp.reduceEvaluationTasks(tasks[1:], leasedWorkers, observer)
	if len(remaining.Items) == vp.items.count() || remaining.VolumeUtilisation() > first.VolumeUtilisation() {
		return remaining
	}
	return first
}

func (vp *volumePacker) reduceEvaluationTasks(tasks []volumeEvaluationTask, workers int, observer evaluationSchedulerObserver) *PackedBox {
	if workers <= 1 {
		var best *PackedBox
		for _, task := range tasks {
			if observer != nil {
				observer(evaluationOrientation, 1)
			}
			result := vp.runEvaluationTask(task)
			if len(result.Items) == vp.items.count() {
				return result
			}
			if best == nil || result.VolumeUtilisation() > best.VolumeUtilisation() {
				best = result
			}
		}
		return best
	}

	jobs := make(chan int, workers)
	completed := make(chan completedVolumeEvaluation, workers)
	var waitGroup sync.WaitGroup
	var active atomic.Int64
	waitGroup.Add(workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			for index := range jobs {
				current := int(active.Add(1))
				if observer != nil {
					observer(evaluationOrientation, current)
				}
				box := vp.runEvaluationTask(tasks[index])
				active.Add(-1)
				completed <- completedVolumeEvaluation{index: index, box: box}
			}
		}()
	}

	results := make([]*PackedBox, len(tasks))
	var best *PackedBox
	for batchStart := 0; batchStart < len(tasks); batchStart += workers {
		batchEnd := minInt(batchStart+workers, len(tasks))
		for index := batchStart; index < batchEnd; index++ {
			jobs <- index
		}
		for range batchEnd - batchStart {
			result := <-completed
			results[result.index] = result.box
		}
		for index := batchStart; index < batchEnd; index++ {
			result := results[index]
			if len(result.Items) == vp.items.count() {
				close(jobs)
				waitGroup.Wait()
				return result
			}
			if best == nil || result.VolumeUtilisation() > best.VolumeUtilisation() {
				best = result
			}
		}
	}

	close(jobs)
	waitGroup.Wait()

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
