package boxpacker

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestPackingSearchReducesKnownGreedyGap(t *testing.T) {
	for _, budget := range []int{0, 64} {
		p := NewPacker()
		p.SetMaxBoxesToBalanceWeight(0)
		p.SetPackingSearchBudget(budget)
		p.AddBox(NewBox("ten", 10, 1, 1, 0, 10, 1, 1, 100))
		supplied := make(map[Item]int)
		for j, width := range []int{6, 5, 3, 2, 2, 2} {
			item := NewItem(fmt.Sprint(j), width, 1, 1, 1, RotationNever)
			p.AddItem(item, 1)
			supplied[item] = 1
		}
		evaluations := 0
		p.packingSearchObserver = func() { evaluations++ }
		boxes, err := p.Pack()
		if err != nil {
			t.Fatal(err)
		}
		want := 3
		if budget > 0 {
			want = 2
		}
		if len(boxes) != want {
			t.Fatalf("budget %d returned %d boxes, want %d", budget, len(boxes), want)
		}
		if evaluations > budget {
			t.Fatalf("used %d solves with budget %d", evaluations, budget)
		}
		assertItemConservation(t, supplied, boxes, p.UnpackedItems())
	}
}

func TestPackingSearchHonoursSupplyAndBudget(t *testing.T) {
	for _, budget := range []int{1, 10, 64} {
		p := NewPacker()
		p.SetPackingSearchBudget(budget)
		p.SetMaxBoxesToBalanceWeight(0)
		large := NewLimitedSupplyBox("large", 10, 1, 1, 0, 10, 1, 1, 100, 1)
		small := NewLimitedSupplyBox("small", 5, 1, 1, 0, 5, 1, 1, 100, 5)
		p.AddBox(large)
		p.AddBox(small)
		supplied := make(map[Item]int)
		for j, width := range []int{5, 4, 3, 2, 2, 2} {
			item := NewItem(fmt.Sprint(j), width, 1, 1, 1, RotationNever)
			supplied[item] = 1
			p.AddItem(item, 1)
		}
		evaluations := 0
		p.packingSearchObserver = func() { evaluations++ }
		boxes, err := p.Pack()
		if err != nil {
			t.Fatal(err)
		}
		counts := make(map[Box]int)
		for _, box := range boxes {
			counts[box.Box]++
		}
		if counts[large] > 1 || counts[small] > 5 {
			t.Fatal("search exceeded supply")
		}
		if p.boxQuantities[large] != 1-counts[large] || p.boxQuantities[small] != 5-counts[small] {
			t.Fatal("remaining supply disagrees with result")
		}
		if evaluations > budget {
			t.Fatal("search exceeded solve budget")
		}
		assertItemConservation(t, supplied, boxes, p.UnpackedItems())
	}
}

func TestPackingSearchPreservesRepeatedIdentityAndReturnedOrder(t *testing.T) {
	p := NewPacker()
	p.SetPackingSearchBudget(128)
	p.SetMaxBoxesToBalanceWeight(0)
	p.AddBox(NewBox("ten", 10, 1, 1, 0, 10, 1, 1, 100))
	a := NewItem("A", 6, 1, 1, 1, RotationNever)
	b := NewItem("B", 5, 1, 1, 1, RotationNever)
	c := NewItem("C", 3, 1, 1, 1, RotationNever)
	d := NewItem("D", 2, 1, 1, 1, RotationNever)
	for item, quantity := range map[Item]int{a: 1, b: 1, c: 1, d: 3} {
		p.AddItem(item, quantity)
	}
	boxes, err := p.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 2 {
		t.Fatalf("got %d boxes, want 2", len(boxes))
	}
	assertItemConservation(t, map[Item]int{a: 1, b: 1, c: 1, d: 3}, boxes, p.UnpackedItems())
	for i, box := range boxes {
		if i > 0 && comparePackedBoxes(boxes[i-1], box) > 0 {
			t.Fatal("result boxes are not sorted")
		}
		for j := 1; j < len(box.Items); j++ {
			if itemVolume(box.Items[j-1].Item) < itemVolume(box.Items[j].Item) {
				t.Fatal("result items are not sorted")
			}
		}
	}
}

func TestPackingSearchSkipsLargePartialAndCustomOrders(t *testing.T) {
	for _, scenario := range []string{"large", "partial", "custom"} {
		t.Run(scenario, func(t *testing.T) {
			p := NewPacker()
			p.SetPackingSearchBudget(64)
			p.SetMaxBoxesToBalanceWeight(0)
			p.AddBox(NewBox("unit", 1, 1, 1, 0, 1, 1, 1, 100))
			item := NewItem("item", 1, 1, 1, 1, RotationNever)
			p.AddItem(item, 2)
			switch scenario {
			case "large":
				p.AddItem(item, 100)
			case "partial":
				p.AllowPartialResults(true)
				p.AddItem(NewItem("too large", 2, 2, 2, 1, RotationNever), 1)
			case "custom":
				p.SetPackedBoxSorter(PackedBoxSorterFunc(comparePackedBoxes))
			}
			p.packingSearchObserver = func() { t.Fatal("search should be skipped") }
			if _, err := p.Pack(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// exactWidthPacking is an independent assignment oracle for tiny 1D cases.
// It explores assignments to bins, without using the package geometry engine.
func exactWidthPacking(widths []int, capacity int) int {
	best := len(widths)
	var loads []int
	var visit func(int)
	visit = func(index int) {
		if index == len(widths) {
			if len(loads) < best {
				best = len(loads)
			}
			return
		}
		if len(loads) >= best {
			return
		}
		tried := make(map[int]bool)
		for i, load := range loads {
			if tried[load] || load+widths[index] > capacity {
				continue
			}
			tried[load] = true
			loads[i] += widths[index]
			visit(index + 1)
			loads[i] -= widths[index]
		}
		loads = append(loads, widths[index])
		visit(index + 1)
		loads = loads[:len(loads)-1]
	}
	visit(0)
	return best
}

func TestPackingSearchAgainstIndependentTinyOracle(t *testing.T) {
	random := rand.New(rand.NewSource(423))
	for scenario := 0; scenario < 40; scenario++ {
		widths := make([]int, 6)
		p := NewPacker()
		p.SetPackingSearchBudget(128)
		p.SetMaxBoxesToBalanceWeight(0)
		p.AddBox(NewBox("ten", 10, 1, 1, 0, 10, 1, 1, 100))
		supplied := make(map[Item]int)
		for j := range widths {
			widths[j] = 1 + random.Intn(9)
			item := NewItem(fmt.Sprint(j), widths[j], 1, 1, 1, RotationNever)
			supplied[item] = 1
			p.AddItem(item, 1)
		}
		boxes, err := p.Pack()
		if err != nil {
			t.Fatal(err)
		}
		if want := exactWidthPacking(widths, 10); len(boxes) != want {
			t.Fatalf("widths %v: got %d boxes, optimum %d", widths, len(boxes), want)
		}
		assertItemConservation(t, supplied, boxes, p.UnpackedItems())
	}
}

func BenchmarkPackingSearchSmallOrder(b *testing.B) {
	for _, budget := range []int{0, 64} {
		b.Run(fmt.Sprint(budget), func(b *testing.B) {
			for b.Loop() {
				p := NewPacker()
				p.SetPackingSearchBudget(budget)
				p.SetMaxBoxesToBalanceWeight(0)
				p.AddBox(NewBox("ten", 10, 1, 1, 0, 10, 1, 1, 100))
				for j, width := range []int{6, 5, 3, 2, 2, 2} {
					p.AddItem(NewItem(fmt.Sprint(j), width, 1, 1, 1, RotationNever), 1)
				}
				if _, err := p.Pack(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
