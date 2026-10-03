package boxpacker

import (
	"sort"
	"testing"
)

// This test-only prototype compares exposed corners plus negative-axis
// projections with the production layer heuristic. It is an exploratory extreme
// point strategy, not a full implementation of the published EP algorithms.
// It deliberately has no production API or effect on the default packer.
type experimentalPoint struct{ x, y, z int }

func experimentalExtremePointPack(box Box, input []Item) *PackedBox {
	items := append([]Item(nil), input...)
	sort.SliceStable(items, func(i, j int) bool { return compareItems(items[i], items[j]) < 0 })
	points := []experimentalPoint{{}}
	packed := &packedItemList{}
	inside := func(point experimentalPoint, item *PackedItem) bool {
		return point.x >= item.X && point.x < item.X+item.Width && point.y >= item.Y && point.y < item.Y+item.Length && point.z >= item.Z && point.z < item.Z+item.Depth
	}
	for _, item := range items {
		if item.Weight() > box.MaxWeight()-box.EmptyWeight()-packed.weight {
			continue
		}
		var best *PackedItem
		for _, point := range points {
			for _, dims := range generatePermutations(item, nil) {
				if point.x+dims[0] > box.InnerWidth() || point.y+dims[1] > box.InnerLength() || point.z+dims[2] > box.InnerDepth() {
					continue
				}
				candidate := &PackedItem{Item: item, X: point.x, Y: point.y, Z: point.z, Width: dims[0], Length: dims[1], Depth: dims[2]}
				overlaps := false
				for _, other := range packed.items {
					if candidate.X < other.X+other.Width && other.X < candidate.X+candidate.Width && candidate.Y < other.Y+other.Length && other.Y < candidate.Y+candidate.Length && candidate.Z < other.Z+other.Depth && other.Z < candidate.Z+candidate.Depth {
						overlaps = true
						break
					}
				}
				if overlaps {
					continue
				}
				if best == nil || candidate.Z < best.Z || (candidate.Z == best.Z && candidate.Y < best.Y) || (candidate.Z == best.Z && candidate.Y == best.Y && candidate.X < best.X) || (candidate.Z == best.Z && candidate.Y == best.Y && candidate.X == best.X && candidate.Depth < best.Depth) {
					best = candidate
				}
			}
		}
		if best == nil {
			continue
		}
		packed.insert(best)
		corners := []experimentalPoint{{best.X + best.Width, best.Y, best.Z}, {best.X, best.Y + best.Length, best.Z}, {best.X, best.Y, best.Z + best.Depth}}
		for _, corner := range corners {
			points = append(points, corner)
			for axis := 0; axis < 3; axis++ {
				projected := corner
				boundary := 0
				for _, other := range packed.items {
					switch axis {
					case 0:
						if corner.y >= other.Y && corner.y < other.Y+other.Length && corner.z >= other.Z && corner.z < other.Z+other.Depth && other.X+other.Width <= corner.x {
							boundary = maxInt(boundary, other.X+other.Width)
						}
					case 1:
						if corner.x >= other.X && corner.x < other.X+other.Width && corner.z >= other.Z && corner.z < other.Z+other.Depth && other.Y+other.Length <= corner.y {
							boundary = maxInt(boundary, other.Y+other.Length)
						}
					case 2:
						if corner.x >= other.X && corner.x < other.X+other.Width && corner.y >= other.Y && corner.y < other.Y+other.Length && other.Z+other.Depth <= corner.z {
							boundary = maxInt(boundary, other.Z+other.Depth)
						}
					}
				}
				switch axis {
				case 0:
					projected.x = boundary
				case 1:
					projected.y = boundary
				case 2:
					projected.z = boundary
				}
				points = append(points, projected)
			}
		}
		seen := make(map[experimentalPoint]bool, len(points))
		kept := points[:0]
		for _, point := range points {
			if seen[point] || point.x >= box.InnerWidth() || point.y >= box.InnerLength() || point.z >= box.InnerDepth() {
				continue
			}
			seen[point] = true
			occupied := false
			for _, other := range packed.items {
				if inside(point, other) {
					occupied = true
					break
				}
			}
			if !occupied {
				kept = append(kept, point)
			}
		}
		points = kept
	}
	return newPackedBox(box, packed)
}

func experimentalPlacementCases() []struct {
	name  string
	box   Box
	items []Item
} {
	ordinary := schedulerBenchmarkVolumePacker(1).inner
	cases := []struct {
		name  string
		box   Box
		items []Item
	}{{"mixed-weight-limited", ordinary.box, ordinary.items.toSlice()}}
	box := NewBox("awkward", 29, 29, 29, 0, 29, 29, 29, 1500)
	var items []Item
	for range 10 {
		items = append(items, NewItem("chunk", 13, 13, 13, 80, RotationBestFit), NewItem("slab", 28, 12, 7, 50, RotationBestFit))
	}
	return append(cases, struct {
		name  string
		box   Box
		items []Item
	}{"awkward-shapes", box, items})
}

func TestExperimentalExtremePointLayoutsAreValid(t *testing.T) {
	for _, scenario := range experimentalPlacementCases() {
		t.Run(scenario.name, func(t *testing.T) {
			result := experimentalExtremePointPack(scenario.box, scenario.items)
			assertPackedBoxValid(t, result)
			if len(result.Items) == 0 {
				t.Fatal("prototype packed no items")
			}
		})
	}
}

func BenchmarkPlacementStrategies(b *testing.B) {
	for _, scenario := range experimentalPlacementCases() {
		for _, strategy := range []string{"layers", "experimental-extreme-points"} {
			b.Run(scenario.name+"/"+strategy, func(b *testing.B) {
				for b.Loop() {
					if strategy == "layers" {
						vp := NewVolumePacker(scenario.box, scenario.items)
						vp.SetMaxConcurrency(1)
						benchmarkPackedBoxSink = vp.Pack()
					} else {
						benchmarkPackedBoxSink = experimentalExtremePointPack(scenario.box, scenario.items)
					}
				}
				b.ReportMetric(float64(len(benchmarkPackedBoxSink.Items)), "items/box")
				b.ReportMetric(benchmarkPackedBoxSink.VolumeUtilisation(), "utilisation-%")
			})
		}
	}
}
