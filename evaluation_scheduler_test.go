package boxpacker

import (
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestEvaluationWorkerBudgets(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	for _, test := range []struct {
		name                                      string
		configured, candidates, work, wantWorkers int
	}{
		{"automatic uses runtime candidate capacity", 0, 6, 600, 4},
		{"configured ceiling", 2, 6, 600, 2},
		{"serial ceiling", 1, 6, 600, 1},
		{"fewer candidates than capacity", 0, 2, 200, 2},
		{"small candidate work stays serial", 0, 2, 2, 1},
		{"no candidates", 0, 0, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := candidateEvaluationWorkers(test.configured, test.candidates, test.work); got != test.wantWorkers {
				t.Fatalf("candidate workers = %d, want %d", got, test.wantWorkers)
			}
		})
	}

	for _, test := range []struct {
		name                     string
		configured, items, tasks int
		singlePass               bool
		wantWorkers              int
	}{
		{"eligible automatic work", 0, 106, 12, false, 2},
		{"configured serial", 1, 106, 12, false, 1},
		{"configured ceiling remains conservative", 4, 106, 12, false, 2},
		{"small work stays serial", 0, 13, 12, false, 1},
		{"single task stays serial", 0, 106, 1, false, 1},
		{"single-pass lookahead stays serial", 0, 106, 12, true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := orientationEvaluationWorkers(test.configured, test.items, test.tasks, test.singlePass); got != test.wantWorkers {
				t.Fatalf("orientation workers = %d, want %d", got, test.wantWorkers)
			}
		})
	}

	runtime.GOMAXPROCS(1)
	if got := candidateEvaluationWorkers(0, 6, 600); got != 1 {
		t.Fatalf("single-CPU candidate workers = %d, want 1", got)
	}
	if got := orientationEvaluationWorkers(0, 106, 12, false); got != 1 {
		t.Fatalf("single-CPU orientation workers = %d, want 1", got)
	}
}

func TestEvaluationWorkerBudgetBlocksAtRuntimeLimit(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	firstLease := sharedEvaluationWorkers.acquire(4)
	firstReleased := false
	t.Cleanup(func() {
		if !firstReleased {
			sharedEvaluationWorkers.release(firstLease)
		}
	})

	secondLease := make(chan int, 1)
	go func() {
		workers := sharedEvaluationWorkers.acquire(2)
		sharedEvaluationWorkers.release(workers)
		secondLease <- workers
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		active, waiting := sharedEvaluationWorkers.snapshot()
		if active == 4 && waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("budget state = active %d, waiting %d; want 4/1", active, waiting)
		}
		runtime.Gosched()
	}
	if workers := sharedEvaluationWorkers.tryAcquire(1); workers != 0 {
		sharedEvaluationWorkers.release(workers)
		t.Fatalf("non-blocking lease acquired %d workers at the runtime limit, want 0", workers)
	}

	sharedEvaluationWorkers.release(firstLease)
	firstReleased = true
	select {
	case workers := <-secondLease:
		if workers != 2 {
			t.Fatalf("second lease = %d workers, want 2", workers)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second lease remained blocked after capacity was released")
	}
	if active, waiting := sharedEvaluationWorkers.snapshot(); active != 0 || waiting != 0 {
		t.Fatalf("final budget state = active %d, waiting %d; want 0/0", active, waiting)
	}
}

func TestEvaluationWorkerBudgetGrantsBlockingLeasesFIFO(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	budget := newEvaluationWorkerBudget()
	initialLease := budget.acquire(2)
	initialReleased := false
	t.Cleanup(func() {
		if !initialReleased {
			budget.release(initialLease)
		}
	})

	type acquiredLease struct {
		waiter  int
		workers int
	}
	acquired := make(chan acquiredLease, 2)
	done := make(chan struct{}, 2)
	releaseWaiter := []chan struct{}{make(chan struct{}, 1), make(chan struct{}, 1)}
	for _, release := range releaseWaiter {
		release := release
		t.Cleanup(func() {
			select {
			case release <- struct{}{}:
			default:
			}
		})
	}

	startWaiter := func(waiter int) {
		go func() {
			workers := budget.acquire(2)
			acquired <- acquiredLease{waiter: waiter, workers: workers}
			<-releaseWaiter[waiter-1]
			budget.release(workers)
			done <- struct{}{}
		}()
	}
	waitForState := func(wantWaiting int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			active, waiting := budget.snapshot()
			if active == 2 && waiting == wantWaiting {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("budget state = active %d, waiting %d; want 2/%d", active, waiting, wantWaiting)
			}
			runtime.Gosched()
		}
	}

	startWaiter(1)
	waitForState(1)
	startWaiter(2)
	waitForState(2)

	budget.release(initialLease)
	initialReleased = true
	if workers := budget.tryAcquire(1); workers != 0 {
		budget.release(workers)
		t.Fatalf("nonblocking lease bypassed queued waiters with %d worker", workers)
	}

	for wantWaiter := 1; wantWaiter <= 2; wantWaiter++ {
		select {
		case lease := <-acquired:
			if lease.waiter != wantWaiter || lease.workers != 2 {
				t.Fatalf("lease %d = waiter %d with %d workers, want waiter %d with 2 workers", wantWaiter, lease.waiter, lease.workers, wantWaiter)
			}
			releaseWaiter[wantWaiter-1] <- struct{}{}
		case <-time.After(2 * time.Second):
			t.Fatalf("waiter %d did not acquire its lease", wantWaiter)
		}
	}
	for range 2 {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("lease waiter did not finish")
		}
	}
	if active, waiting := budget.snapshot(); active != 0 || waiting != 0 {
		t.Fatalf("final budget state = active %d, waiting %d; want 0/0", active, waiting)
	}
}

func TestSetMaxConcurrencySemantics(t *testing.T) {
	packer := NewPacker()
	packer.SetMaxConcurrency(3)
	if packer.maxConcurrency != 3 {
		t.Fatalf("Packer maximum = %d, want 3", packer.maxConcurrency)
	}
	packer.SetMaxConcurrency(-1)
	if packer.maxConcurrency != 0 {
		t.Fatalf("negative Packer maximum = %d, want adaptive 0", packer.maxConcurrency)
	}

	volumePacker := NewVolumePacker(NewBox("Box", 10, 10, 10, 0, 10, 10, 10, 100), nil)
	volumePacker.SetMaxConcurrency(2)
	if volumePacker.inner.maxConcurrency != 2 {
		t.Fatalf("VolumePacker maximum = %d, want 2", volumePacker.inner.maxConcurrency)
	}
	volumePacker.SetMaxConcurrency(-1)
	if volumePacker.inner.maxConcurrency != 0 {
		t.Fatalf("negative VolumePacker maximum = %d, want adaptive 0", volumePacker.inner.maxConcurrency)
	}
}

func TestAdaptiveSchedulerRespectsConfiguredCeiling(t *testing.T) {
	t.Run("orientation", func(t *testing.T) {
		packer := schedulerTestPacker(2, true)
		var maximum atomic.Int64
		var sawOrientation atomic.Bool
		packer.schedulerObserver = func(kind evaluationKind, active int) {
			if kind == evaluationOrientation {
				sawOrientation.Store(true)
				storeMaximum(&maximum, int64(active))
			}
		}
		if _, err := packer.Pack(); err != nil {
			t.Fatal(err)
		}
		if !sawOrientation.Load() {
			t.Fatal("expected orientation scheduler activity")
		}
		if got := maximum.Load(); got > 2 {
			t.Fatalf("orientation concurrency = %d, want at most 2", got)
		}
	})

	t.Run("candidate", func(t *testing.T) {
		packer := NewPacker()
		packer.SetMaxConcurrency(2)
		packer.SetMaxBoxesToBalanceWeight(0)
		for size := 100; size <= 250; size += 50 {
			packer.AddBox(NewBox("Box", size, size, size, 0, size, size, size, 100_000))
		}
		packer.AddItem(NewItem("Item", 40, 30, 20, 100, RotationBestFit), 30)

		var maximum atomic.Int64
		var sawCandidate atomic.Bool
		packer.schedulerObserver = func(kind evaluationKind, active int) {
			if kind == evaluationCandidate {
				sawCandidate.Store(true)
				storeMaximum(&maximum, int64(active))
			}
		}
		if _, err := packer.Pack(); err != nil {
			t.Fatal(err)
		}
		if !sawCandidate.Load() {
			t.Fatal("expected candidate scheduler activity")
		}
		if got := maximum.Load(); got > 2 {
			t.Fatalf("candidate concurrency = %d, want at most 2", got)
		}
	})
}

func TestAdaptiveSchedulerPreservesDeterministicResults(t *testing.T) {
	baselinePacker := schedulerTestPacker(1, true)
	baseline, err := baselinePacker.Pack()
	if err != nil {
		t.Fatal(err)
	}
	want := phpParityResult(baseline)

	for _, maxConcurrency := range []int{0, 1, 2, 4} {
		for run := 0; run < 10; run++ {
			packer := schedulerTestPacker(maxConcurrency, true)
			got, err := packer.Pack()
			if err != nil {
				t.Fatalf("maximum %d run %d: %v", maxConcurrency, run, err)
			}
			if result := phpParityResult(got); !reflect.DeepEqual(result, want) {
				t.Fatalf("maximum %d run %d differs:\ngot=%v\nwant=%v", maxConcurrency, run, result, want)
			}
		}
	}
}

func TestParallelReductionPreservesEarliestCompleteFit(t *testing.T) {
	box := NewBox("cube", 500, 500, 500, 0, 500, 500, 500, 10_000)
	item := NewItem("item", 10, 20, 30, 1, RotationBestFit)
	items := make([]Item, 100)
	for index := range items {
		items[index] = item
	}

	serial := NewVolumePacker(box, items)
	serial.SetMaxConcurrency(1)
	want := phpParityResult([]*PackedBox{serial.Pack()})

	parallel := NewVolumePacker(box, items)
	parallel.SetMaxConcurrency(2)
	var evaluations atomic.Int64
	parallel.inner.schedulerObserver = func(kind evaluationKind, _ int) {
		if kind == evaluationOrientation {
			evaluations.Add(1)
		}
	}
	got := phpParityResult([]*PackedBox{parallel.Pack()})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parallel complete-fit reduction differs:\ngot=%v\nwant=%v", got, want)
	}
	if got := evaluations.Load(); got != 1 {
		t.Fatalf("complete-fit path evaluated %d orientations, want exactly 1", got)
	}
}

func TestSinglePassLookaheadDoesNotScheduleRecursively(t *testing.T) {
	items := &itemList{}
	items.insert(NewItem("Item", 10, 20, 30, 1, RotationBestFit), 100)
	packer := newVolumePacker(&workingVolume{width: 100, length: 100, depth: 100}, items)
	packer.setSinglePassMode(true)

	var maximum atomic.Int64
	packer.schedulerObserver = func(_ evaluationKind, active int) {
		storeMaximum(&maximum, int64(active))
	}
	packer.pack()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("single-pass concurrency = %d, want exactly 1", got)
	}
}

func schedulerTestPacker(maxConcurrency int, shortCircuit bool) *Packer {
	packer := NewPacker()
	packer.SetMaxConcurrency(maxConcurrency)
	packer.SetQuantityShortCircuit(shortCircuit)
	packer.SetMaxBoxesToBalanceWeight(0)
	packer.AddBox(NewBox("L", 134, 175, 156, 0, 134, 175, 156, 10_032))
	packer.AddItem(NewItem("I0", 48, 65, 115, 580, RotationBestFit), 20)
	packer.AddItem(NewItem("I1", 18, 61, 111, 1_002, RotationBestFit), 46)
	packer.AddItem(NewItem("I2", 86, 84, 67, 1_389, RotationBestFit), 40)
	return packer
}

func storeMaximum(maximum *atomic.Int64, value int64) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}
