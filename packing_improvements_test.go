package boxpacker

import (
	"fmt"
	"math/rand"
	"slices"
	"sync"
	"testing"
)

func assertItemConservation(t *testing.T, supplied map[Item]int, boxes []*PackedBox, unpacked []Item) {
	t.Helper()
	actual := make(map[Item]int)
	placements := make(map[*PackedItem]bool)
	for _, box := range boxes {
		assertPackedBoxValid(t, box)
		for _, item := range box.Items {
			actual[item.Item]++
			if placements[item] {
				t.Fatal("separate placements share a mutable pointer")
			}
			placements[item] = true
		}
	}
	for _, item := range unpacked {
		actual[item]++
	}
	if len(actual) != len(supplied) {
		t.Fatalf("identities: got %d, want %d", len(actual), len(supplied))
	}
	for item, count := range supplied {
		if actual[item] != count {
			t.Fatalf("identity %p (%s): got %d, want %d", item, item.Description(), actual[item], count)
		}
	}
}

func TestPrecedingOrientationRespectsCurrentPolicy(t *testing.T) {
	for _, policy := range []Rotation{RotationNever, RotationKeepFlat} {
		t.Run(policy.String(), func(t *testing.T) {
			p := NewPacker()
			p.AllowPartialResults(true)
			p.AddBox(NewBox("box", 8, 2, 1, 0, 8, 2, 1, 100))
			first := NewItem("A", 2, 4, 1, 2, RotationBestFit)
			w, l, d := 2, 4, 1
			if policy == RotationKeepFlat {
				w, l, d = 2, 1, 4
			}
			second := NewItem("B", w, l, d, 1, policy)
			p.AddItem(first, 1)
			p.AddItem(second, 1)
			boxes, err := p.Pack()
			if err != nil {
				t.Fatal(err)
			}
			if totalPackedItems(boxes) != 1 || len(p.UnpackedItems()) != 1 {
				t.Fatal("restricted item should not fit")
			}
			assertItemConservation(t, map[Item]int{first: 1, second: 1}, boxes, p.UnpackedItems())
		})
	}
}

func TestReplicationPreservesDistinctIdentities(t *testing.T) {
	for _, shortCircuit := range []bool{false, true} {
		t.Run(fmt.Sprint(shortCircuit), func(t *testing.T) {
			p := NewPacker()
			p.SetQuantityShortCircuit(shortCircuit)
			p.SetMaxBoxesToBalanceWeight(0)
			p.AddBox(NewBox("box", 4, 2, 1, 0, 4, 2, 1, 100))
			supplied := make(map[Item]int)
			for j := 0; j < 40; j++ {
				for _, width := range []int{1, 3} {
					item := NewItem(fmt.Sprint(width), width, 2, 1, 1, RotationNever)
					quantity := 1 + j%3
					p.AddItem(item, quantity)
					supplied[item] = quantity
				}
			}
			boxes, err := p.Pack()
			if err != nil {
				t.Fatal(err)
			}
			assertItemConservation(t, supplied, boxes, p.UnpackedItems())
		})
	}
}

func TestSkippingRespectsDifferentRotationPolicies(t *testing.T) {
	p := NewPacker()
	p.AllowPartialResults(true)
	p.AddBox(NewBox("box", 8, 2, 1, 0, 8, 2, 1, 100))
	fixed := NewItem("fixed", 2, 4, 1, 3, RotationNever)
	rotatable := NewItem("rotatable", 2, 4, 1, 2, RotationBestFit)
	small := NewItem("small", 1, 1, 1, 1, RotationNever)
	p.AddItem(fixed, 1)
	p.AddItem(rotatable, 1)
	p.AddItem(small, 1)
	boxes, err := p.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if totalPackedItems(boxes) != 2 {
		t.Fatal("packable item was skipped because a restricted item had the same dimensions")
	}
	assertItemConservation(t, map[Item]int{fixed: 1, rotatable: 1, small: 1}, boxes, p.UnpackedItems())
}

func TestCompactItemsPreserveStableOrderAndCloneIsolation(t *testing.T) {
	a := NewItem("tie", 1, 2, 3, 1, RotationNever)
	b := NewItem("tie", 3, 2, 1, 1, RotationNever)
	l := &itemList{}
	l.insert(a, 1_000_000)
	if len(l.runs) != 1 || l.count() != 1_000_000 {
		t.Fatal("quantity was expanded")
	}
	c := l.clone()
	c.removeFirstN(999_999)
	if l.count() != 1_000_000 || c.count() != 1 {
		t.Fatal("clone changed original")
	}
	l = &itemList{}
	l.insert(a, 2)
	l.insert(b, 1)
	l.insert(a, 2)
	if !slices.Equal(l.toSlice(), []Item{a, a, b, a, a}) {
		t.Fatal("stable ties changed insertion order")
	}
	l.removePackedItems([]*PackedItem{{Item: a}, {Item: b}, {Item: a}})
	if !slices.Equal(l.toSlice(), []Item{a, a}) {
		t.Fatal("bulk removal lost identities or counts")
	}
}

func TestSchedulerEstimateDoesNotExpandWithQuantity(t *testing.T) {
	box := NewBox("box", 10, 10, 10, 0, 10, 10, 10, 100)
	item := NewItem("item", 5, 5, 5, 1, RotationBestFit)
	for _, quantity := range []int{100, 100_000} {
		items := &itemList{}
		items.insert(item, quantity)
		if got := newVolumePacker(box, items).estimatedWork(); got != 9 {
			t.Fatalf("quantity %d estimates %d work, want one run plus eight placements", quantity, got)
		}
	}
}

func TestRandomizedPackingConservesItems(t *testing.T) {
	random := rand.New(rand.NewSource(741))
	for scenario := 0; scenario < 150; scenario++ {
		p := NewPacker()
		p.SetQuantityShortCircuit(scenario%2 == 0)
		p.AllowPartialResults(true)
		p.SetMaxConcurrency(1 + scenario%3)
		p.AddBox(NewBox("box", 10, 11, 12, 2, 10, 11, 12, 90))
		supplied := make(map[Item]int)
		for j := 0; j < 6; j++ {
			rotation := []Rotation{RotationNever, RotationKeepFlat, RotationBestFit}[random.Intn(3)]
			item := NewItem(fmt.Sprint(j%3), 1+random.Intn(13), 1+random.Intn(13), 1+random.Intn(13), 1+random.Intn(25), rotation)
			quantity := 1 + random.Intn(15)
			p.AddItem(item, quantity)
			supplied[item] = quantity
		}
		boxes, err := p.Pack()
		if err != nil {
			t.Fatal(err)
		}
		assertItemConservation(t, supplied, boxes, p.UnpackedItems())
	}
}

func TestBoundedCacheConcurrentEviction(t *testing.T) {
	cache := newBoundedCache(8)
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for i := 0; i < 100; i++ {
				key := fmt.Sprint(worker*100 + i)
				cache.Store(key, i)
				if got, ok := cache.Load(key); ok && got != i {
					t.Errorf("cache returned wrong value")
				}
			}
		}(worker)
	}
	workers.Wait()
	count := 0
	cache.values.Range(func(_, _ any) bool { count++; return true })
	if count != 8 || len(cache.keys) != 8 {
		t.Fatalf("cache retains %d entries and %d keys", count, len(cache.keys))
	}
}

func BenchmarkCompactQuantityInput(b *testing.B) {
	item := NewItem("item", 1, 1, 1, 1, RotationBestFit)
	for _, quantity := range []int{1_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprint(quantity), func(b *testing.B) {
			for b.Loop() {
				p := NewPacker()
				p.AddItem(item, quantity)
				p.items.clone()
				p.items.signatureCounts()
			}
		})
	}
}
