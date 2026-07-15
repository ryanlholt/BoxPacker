package boxpacker

import (
	"fmt"
	"runtime"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkAdaptiveFirstOrientationScheduler(b *testing.B) {
	for _, multiplier := range []int{1, 10} {
		workload := "ordinary"
		if multiplier > 1 {
			workload = "uncapped-large-pool"
		}
		for _, maxConcurrency := range []int{1, 0} {
			mode := "serial"
			if maxConcurrency == 0 {
				mode = "adaptive"
			}
			b.Run(workload+"/"+mode, func(b *testing.B) {
				for b.Loop() {
					packer := schedulerBenchmarkVolumePacker(multiplier)
					packer.SetMaxConcurrency(maxConcurrency)
					benchmarkPackedBoxSink = packer.Pack()
				}
			})
		}
	}
}

func BenchmarkAdaptiveManyCandidateScheduler(b *testing.B) {
	for _, maxConcurrency := range []int{1, 0} {
		mode := "serial"
		if maxConcurrency == 0 {
			mode = "adaptive"
		}
		b.Run(mode, func(b *testing.B) {
			for b.Loop() {
				packer := schedulerBenchmarkManyCandidates(maxConcurrency)
				benchmarkPackedBoxesSink, benchmarkErrorSink = packer.Pack()
				if benchmarkErrorSink != nil {
					b.Fatal(benchmarkErrorSink)
				}
			}
		})
	}
}

func BenchmarkAdaptiveQuantityModes(b *testing.B) {
	for _, shortCircuit := range []bool{false, true} {
		workload := "uncapped-short-circuit-off"
		if shortCircuit {
			workload = "bounded-short-circuit-on"
		}
		for _, maxConcurrency := range []int{1, 0} {
			mode := "serial"
			if maxConcurrency == 0 {
				mode = "adaptive"
			}
			b.Run(workload+"/"+mode, func(b *testing.B) {
				for b.Loop() {
					packer := schedulerTestPacker(maxConcurrency, shortCircuit)
					benchmarkPackedBoxesSink, benchmarkErrorSink = packer.Pack()
					if benchmarkErrorSink != nil {
						b.Fatal(benchmarkErrorSink)
					}
				}
			})
		}
	}
}

func BenchmarkAdaptiveParallelRequests(b *testing.B) {
	for _, maxConcurrency := range []int{1, 0} {
		mode := "serial-internal"
		if maxConcurrency == 0 {
			mode = "adaptive-internal"
		}
		b.Run(mode, func(b *testing.B) {
			latencies := make([]int64, b.N)
			var sampleIndex atomic.Int64
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					packer := schedulerBenchmarkVolumePacker(1)
					packer.SetMaxConcurrency(maxConcurrency)
					started := time.Now()
					packedBox := packer.Pack()
					index := int(sampleIndex.Add(1) - 1)
					if index < len(latencies) {
						latencies[index] = time.Since(started).Nanoseconds()
					}
					runtime.KeepAlive(packedBox)
				}
			})
			b.StopTimer()
			sampleCount := minInt(int(sampleIndex.Load()), len(latencies))
			if sampleCount > 0 {
				sort.Slice(latencies[:sampleCount], func(i, j int) bool { return latencies[i] < latencies[j] })
				p95Index := (sampleCount*95+99)/100 - 1
				b.ReportMetric(float64(latencies[p95Index]), "p95-ns")
			}
		})
	}
}

var (
	benchmarkPackedBoxSink   *PackedBox
	benchmarkPackedBoxesSink []*PackedBox
	benchmarkErrorSink       error
)

func schedulerBenchmarkVolumePacker(multiplier int) *VolumePacker {
	box := NewBox("L", 134, 175, 156, 0, 134, 175, 156, 10_032)
	items := make([]Item, 0, 106*multiplier)
	appendItems := func(item Item, quantity int) {
		for range quantity * multiplier {
			items = append(items, item)
		}
	}
	appendItems(NewItem("I0", 48, 65, 115, 580, RotationBestFit), 20)
	appendItems(NewItem("I1", 18, 61, 111, 1_002, RotationBestFit), 46)
	appendItems(NewItem("I2", 86, 84, 67, 1_389, RotationBestFit), 40)
	return NewVolumePacker(box, items)
}

func schedulerBenchmarkManyCandidates(maxConcurrency int) *Packer {
	packer := NewPacker()
	packer.SetMaxConcurrency(maxConcurrency)
	packer.SetMaxBoxesToBalanceWeight(0)
	for size := 50; size <= 500; size += 25 {
		packer.AddBox(NewBox(fmt.Sprintf("cube-%d", size), size+10, size+10, size+10, 100, size, size, size, 100_000))
	}
	packer.AddItem(NewItem("widget", 100, 100, 50, 100, RotationBestFit), 4)
	packer.AddItem(NewItem("gadget", 50, 50, 50, 50, RotationBestFit), 6)
	return packer
}
