package boxpacker

import (
	"runtime"
	"sync"
)

const (
	// Candidate solves have enough fixed goroutine overhead that very small
	// post-cap item pools are faster inline.
	minimumParallelCandidateWork = 32

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

// evaluationWorkerBudget is a process-wide lease for active evaluation
// workers. A pool may receive fewer workers than it requested, and waits when
// every GOMAXPROCS slot is already leased by other pack calls.
type evaluationWorkerBudget struct {
	mutex     sync.Mutex
	condition *sync.Cond
	active    int
	waiting   int
}

func newEvaluationWorkerBudget() *evaluationWorkerBudget {
	budget := &evaluationWorkerBudget{}
	budget.condition = sync.NewCond(&budget.mutex)
	return budget
}

var sharedEvaluationWorkers = newEvaluationWorkerBudget()

func (b *evaluationWorkerBudget) acquire(requested int) int {
	if requested < 1 {
		return 0
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()
	limit := runtime.GOMAXPROCS(0)
	for b.active >= limit {
		b.waiting++
		b.condition.Wait()
		b.waiting--
		limit = runtime.GOMAXPROCS(0)
	}
	granted := minInt(requested, limit-b.active)
	b.active += granted
	return granted
}

func (b *evaluationWorkerBudget) tryAcquire(requested int) int {
	if requested < 1 {
		return 0
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	available := runtime.GOMAXPROCS(0) - b.active
	if available < 1 {
		return 0
	}
	granted := minInt(requested, available)
	b.active += granted
	return granted
}

func (b *evaluationWorkerBudget) release(workers int) {
	if workers < 1 {
		return
	}
	b.mutex.Lock()
	b.active -= workers
	b.condition.Broadcast()
	b.mutex.Unlock()
}

func (b *evaluationWorkerBudget) snapshot() (active, waiting int) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.active, b.waiting
}

func configuredConcurrencyLimit(maxConcurrency int) int {
	limit := runtime.GOMAXPROCS(0)
	if limit < 1 {
		limit = 1
	}
	if maxConcurrency > 0 && maxConcurrency < limit {
		limit = maxConcurrency
	}
	return limit
}

func candidateEvaluationWorkers(maxConcurrency, candidateCount, estimatedWork int) int {
	if candidateCount < 1 {
		return 0
	}
	if candidateCount == 1 || estimatedWork < minimumParallelCandidateWork {
		return 1
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
