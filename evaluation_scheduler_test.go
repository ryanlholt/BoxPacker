package boxpacker

import (
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestEvaluationWorkerBudgets(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	for _, test := range []struct {
		name                                string
		configured, candidates, wantWorkers int
	}{
		{"automatic uses runtime candidate capacity", 0, 6, 4},
		{"configured ceiling", 2, 6, 2},
		{"serial ceiling", 1, 6, 1},
		{"fewer candidates than capacity", 0, 2, 2},
		{"no candidates", 0, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := candidateEvaluationWorkers(test.configured, test.candidates); got != test.wantWorkers {
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
	if got := candidateEvaluationWorkers(0, 6); got != 1 {
		t.Fatalf("single-CPU candidate workers = %d, want 1", got)
	}
	if got := orientationEvaluationWorkers(0, 106, 12, false); got != 1 {
		t.Fatalf("single-CPU orientation workers = %d, want 1", got)
	}
}

func TestAdaptiveSchedulerSharesRuntimeAcrossPackingCalls(t *testing.T) {
	previous := runtime.GOMAXPROCS(8)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	activePackingCalls.Store(4)
	t.Cleanup(func() { activePackingCalls.Store(0) })

	if got := candidateEvaluationWorkers(0, 10); got != 2 {
		t.Fatalf("automatic workers = %d, want fair share 2", got)
	}
	if got := candidateEvaluationWorkers(4, 10); got != 2 {
		t.Fatalf("configured workers = %d, want runtime-limited fair share 2", got)
	}
	if got := orientationEvaluationWorkers(0, 106, 12, false); got != 2 {
		t.Fatalf("orientation workers = %d, want fair share 2", got)
	}

	activePackingCalls.Store(8)
	if got := candidateEvaluationWorkers(0, 10); got != 1 {
		t.Fatalf("saturated workers = %d, want serial 1", got)
	}
	if got := orientationEvaluationWorkers(0, 106, 12, false); got != 1 {
		t.Fatalf("saturated orientation workers = %d, want serial 1", got)
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
	got := phpParityResult([]*PackedBox{parallel.Pack()})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parallel complete-fit reduction differs:\ngot=%v\nwant=%v", got, want)
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
