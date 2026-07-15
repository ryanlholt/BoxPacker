package boxpacker

import (
	"runtime"
	"sync/atomic"
)

const (
	// Parallel orientation solves clone and traverse the item pool. Below this
	// amount of estimated work, scheduler overhead is more expensive than the
	// useful CPU overlap on the benchmarked workloads.
	minimumParallelOrientationWork = 512

	// Orientation packing is memory-intensive. Two concurrent solves improved
	// representative workloads; wider fan-out saturated memory bandwidth.
	maximumAutomaticOrientationWorkers = 2
)

type evaluationKind uint8

const (
	evaluationCandidate evaluationKind = iota
	evaluationOrientation
)

// evaluationSchedulerObserver is an internal test hook. active is the number
// of tasks currently executing in the one active scheduler pool.
type evaluationSchedulerObserver func(kind evaluationKind, active int)

// activePackingCalls lets automatic scheduling share the runtime budget across
// independent callers. It is deliberately process-local and advisory: an
// explicit maximum remains a per-instance ceiling, while a busy process may
// use fewer workers than that ceiling.
var activePackingCalls atomic.Int64

func beginPackingCall() func() {
	activePackingCalls.Add(1)
	return func() {
		activePackingCalls.Add(-1)
	}
}

func configuredConcurrencyLimit(maxConcurrency int) int {
	activeCalls := int(activePackingCalls.Load())
	if activeCalls < 1 {
		activeCalls = 1
	}
	limit := runtime.GOMAXPROCS(0) / activeCalls
	if limit < 1 {
		limit = 1
	}
	if maxConcurrency > 0 && maxConcurrency < limit {
		limit = maxConcurrency
	}
	return limit
}

func candidateEvaluationWorkers(maxConcurrency, candidateCount int) int {
	if candidateCount < 1 {
		return 0
	}
	return minInt(candidateCount, configuredConcurrencyLimit(maxConcurrency))
}

func orientationEvaluationWorkers(maxConcurrency, itemCount, taskCount int, singlePass bool) int {
	if taskCount < 1 {
		return 0
	}
	limit := configuredConcurrencyLimit(maxConcurrency)
	if singlePass || limit == 1 || taskCount == 1 || itemCount*taskCount < minimumParallelOrientationWork {
		return 1
	}
	return minInt(taskCount, minInt(limit, maximumAutomaticOrientationWorkers))
}
