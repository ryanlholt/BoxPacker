package boxpacker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func cancellationFixture() *Packer {
	p := NewPacker()
	p.SetMaxBoxesToBalanceWeight(0)
	p.AddBox(NewBox("cube", 10, 10, 10, 0, 10, 10, 10, 10000))
	p.AddItem(NewItem("toy", 1, 1, 1, 1, RotationBestFit), 200)
	return p
}

func TestPackContextCanceledBeforeWork(t *testing.T) {
	for _, partial := range []bool{false, true} {
		p := cancellationFixture()
		p.AllowPartialResults(partial)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		boxes, err := p.PackContext(ctx)
		if !errors.Is(err, context.Canceled) || len(boxes) != 0 || p.items.count() != 200 {
			t.Fatalf("canceled pack changed state: %v %d", err, p.items.count())
		}
		if _, err := p.Pack(); err != nil {
			t.Fatal("context leaked into subsequent Pack:", err)
		}
	}
	vp := NewVolumePacker(NewBox("cube", 2, 2, 2, 0, 2, 2, 2, 10), []Item{NewItem("toy", 1, 1, 1, 1, RotationBestFit)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if box, err := vp.PackContext(ctx); box != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("volume result=%v err=%v", box, err)
	}
	if box := vp.Pack(); len(box.Items) != 1 {
		t.Fatal("volume context leaked")
	}
}

func TestPackContextBackgroundPreservesResults(t *testing.T) {
	a, err := cancellationFixture().Pack()
	if err != nil {
		t.Fatal(err)
	}
	b, err := cancellationFixture().PackContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("context changed ordinary packing")
	}
}

func waitBudget(t *testing.T, budget *evaluationWorkerBudget, wantWaiting int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, waiting := budget.snapshot()
		if waiting == wantWaiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting=%d want=%d", waiting, wantWaiting)
		}
		runtime.Gosched()
	}
}

func TestCanceledWorkerWaitersDoNotBlockFIFO(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, cancelFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelFirst), func(t *testing.T) {
			b := newEvaluationWorkerBudget()
			lease := b.acquire(1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			canceled := make(chan error, 1)
			survivor := make(chan int, 1)
			startCanceled := func() {
				go func() {
					workers, err := b.acquireContext(ctx, 1)
					if workers > 0 {
						b.release(workers)
					}
					canceled <- err
				}()
			}
			startSurvivor := func() { go func() { workers := b.acquire(1); b.release(workers); survivor <- workers }() }
			if cancelFirst {
				startCanceled()
				waitBudget(t, b, 1)
				startSurvivor()
			} else {
				startSurvivor()
				waitBudget(t, b, 1)
				startCanceled()
			}
			waitBudget(t, b, 2)
			cancel()
			select {
			case err := <-canceled:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("waiter did not cancel while slot was occupied")
			}
			waitBudget(t, b, 1)
			b.release(lease)
			select {
			case workers := <-survivor:
				if workers != 1 {
					t.Fatal(workers)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("canceled ticket blocked survivor")
			}
			if a, w := b.snapshot(); a != 0 || w != 0 {
				t.Fatalf("leaked worker state %d/%d", a, w)
			}
		})
	}
}

func TestPackContextDeadlineWhileWaitingForWorkers(t *testing.T) {
	lease := sharedEvaluationWorkers.acquire(runtime.GOMAXPROCS(0))
	defer sharedEvaluationWorkers.release(lease)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	p := cancellationFixture()
	start := time.Now()
	boxes, err := p.PackContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || len(boxes) != 0 || p.items.count() != 200 {
		t.Fatalf("bad interrupted pack: %v %d", err, p.items.count())
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation did not unblock worker acquisition")
	}
}

// Cancels from inside placement rather than before candidate evaluation.
type cancelingItem struct {
	Item
	calls  atomic.Int64
	cancel context.CancelFunc
}

func (i *cancelingItem) Weight() int {
	if i.calls.Add(1) == 100 {
		i.cancel()
	}
	return i.Item.Weight()
}

func TestPackContextStopsPlacementAndReleasesWorkers(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := cancellationFixture()
	p.items = &itemList{}
	item := &cancelingItem{Item: NewItem("cancel during placement", 1, 1, 1, 1, RotationBestFit), cancel: cancel}
	p.AddItem(item, 10000)
	p.AddBox(NewBox("other", 11, 11, 11, 0, 11, 11, 11, 10000))
	_, err := p.PackContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if item.calls.Load() < 100 || item.calls.Load() > 2000 {
		t.Fatalf("placement kept running: %d calls", item.calls.Load())
	}
	if p.items.count() != 10000 {
		t.Fatal("interrupted candidate was committed")
	}
	if a, w := sharedEvaluationWorkers.snapshot(); a != 0 || w != 0 {
		t.Fatalf("leaked workers %d/%d", a, w)
	}
	if _, err := cancellationFixture().Pack(); err != nil {
		t.Fatal("next request failed:", err)
	}
}

func TestCancellationDuringSearchPreservesIncumbentSupply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPacker()
	p.SetMaxBoxesToBalanceWeight(0)
	p.SetPackingSearchBudget(64)
	box := NewLimitedSupplyBox("ten", 10, 1, 1, 0, 10, 1, 1, 100, 10)
	p.AddBox(box)
	for n, width := range []int{6, 5, 3, 2, 2, 2} {
		p.AddItem(NewItem(fmt.Sprint(n), width, 1, 1, 1, RotationNever), 1)
	}
	p.packingSearchObserver = cancel
	boxes, err := p.PackContext(ctx)
	if !errors.Is(err, context.Canceled) || len(boxes) != 3 || p.boxQuantities[box] != 7 {
		t.Fatalf("canceled search committed alternative: %v boxes=%d supply=%d", err, len(boxes), p.boxQuantities[box])
	}
}

func TestCancellationDuringReplicationDoesNotConsumeTail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPacker()
	p.ctx = ctx
	box := NewLimitedSupplyBox("one", 1, 1, 1, 0, 1, 1, 1, 100, 1000)
	p.AddBox(box)
	item := &cancelingItem{Item: NewItem("toy", 1, 1, 1, 1, RotationNever), cancel: cancel}
	p.AddItem(item, 1000)
	template := newPackedBox(box, &packedItemList{items: []*PackedItem{{Item: item, Width: 1, Length: 1, Depth: 1}}})
	// Signature/clone assignment queries Weight once per copy.
	replicas := p.replicateIdenticalBoxes(template)
	if contextError(ctx) == nil || len(replicas) != 0 || p.items.count() != 1000 || p.boxQuantities[box] != 1000 {
		t.Fatalf("replication committed on cancellation: %d %d", len(replicas), p.items.count())
	}
}

func TestVolumeContextJoinsParallelOrientationWorkers(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := make([]Item, 200)
	for i := range items {
		items[i] = NewItem(fmt.Sprint(i), 2, 3, 4, 1, RotationBestFit)
	}
	vp := NewVolumePacker(NewBox("cube", 7, 8, 9, 0, 7, 8, 9, 10000), items)
	var parallel atomic.Bool
	var observations atomic.Int32
	barrier := make(chan struct{})
	var release sync.Once
	vp.inner.schedulerObserver = func(kind evaluationKind, active int) {
		if observations.Add(1) == 1 {
			return
		} // First orientation remains serial.
		if active >= 2 {
			parallel.Store(true)
			cancel()
			release.Do(func() { close(barrier) })
		}
		<-barrier
	}
	done := make(chan error, 1)
	go func() { _, err := vp.PackContext(ctx); done <- err }()
	select {
	case err := <-done:
		if !parallel.Load() {
			t.Fatal("orientation pool did not overlap evaluations")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		release.Do(func() { close(barrier) })
		<-done
		t.Fatal("orientation cancellation hung")
	}
	if a, w := sharedEvaluationWorkers.snapshot(); a != 0 || w != 0 {
		t.Fatalf("orientation workers leaked %d/%d", a, w)
	}
}

func TestCancellationDuringBalancingStopsRepacking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPacker()
	p.SetMaxBoxesToBalanceWeight(0)
	box := NewLimitedSupplyBox("stack", 1, 1, 3, 0, 1, 1, 3, 3, 10)
	p.AddBox(box)
	item := &cancelingItem{Item: NewItem("toy", 1, 1, 1, 1, RotationBestFit), cancel: cancel}
	p.AddItem(item, 4)
	original, err := p.Pack()
	if err != nil {
		t.Fatal(err)
	}
	// The next balancing attempt triggers cancellation while examining/repacking
	// item sets. No speculative supply changes may be committed afterwards.
	item.calls.Store(99)
	r := newWeightRedistributor(p.boxes, p.boxSorter, p.boxQuantities, 1)
	r.ctx = ctx
	result := r.redistributeWeight(original)
	if contextError(ctx) == nil {
		t.Fatal("balancing did not reach cancellation")
	}
	if len(result) != 2 || p.boxQuantities[box] != 8 {
		t.Fatal("canceled balancing changed supply")
	}
	if a, w := sharedEvaluationWorkers.snapshot(); a != 0 || w != 0 {
		t.Fatalf("balancing workers leaked %d/%d", a, w)
	}
}
