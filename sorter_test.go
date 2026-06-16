package boxpacker

import (
	"fmt"
	"testing"
)

func TestVolumetricWeight(t *testing.T) {
	// 300 x 200 x 150 mm = 9,000,000 mm^3; divisor 5000 -> 1800
	box := NewBox("parcel", 300, 200, 150, 0, 290, 190, 140, 100000)
	if got := VolumetricWeight(box, 5000); got != 1800 {
		t.Fatalf("VolumetricWeight = %v, want 1800", got)
	}
	if got := VolumetricWeight(box, 0); got != 0 {
		t.Fatalf("VolumetricWeight with non-positive divisor = %v, want 0", got)
	}
}

func TestBillableWeight(t *testing.T) {
	box := NewBox("parcel", 300, 200, 150, 200, 290, 190, 140, 100000)

	// Heavy contents: actual weight dominates.
	heavy := newPackedBox(box, &packedItemList{weight: 5000})
	if got := BillableWeight(heavy, 5000); got != 5200 { // 5000 items + 200 empty
		t.Fatalf("BillableWeight (actual-dominated) = %v, want 5200", got)
	}

	// Light contents: volumetric weight (1800) dominates actual (100 + 200).
	light := newPackedBox(box, &packedItemList{weight: 100})
	if got := BillableWeight(light, 5000); got != 1800 {
		t.Fatalf("BillableWeight (volumetric-dominated) = %v, want 1800", got)
	}
}

// minBillableWeightSorter prefers the parcel a carrier would charge least for,
// breaking ties towards more items (so it still consolidates when shipping cost
// is equal).
type minBillableWeightSorter struct{ divisor float64 }

func (s minBillableWeightSorter) Compare(a, b *PackedBox) int {
	aw, bw := BillableWeight(a, s.divisor), BillableWeight(b, s.divisor)
	switch {
	case aw < bw:
		return -1
	case aw > bw:
		return 1
	}
	return comparePackedBoxes(a, b) // tie-break: default (most items, fullest)
}

// TestCustomSorterChangesSelection shows the hook actually changes the outcome:
// with light, bulky items the default "most items" objective consolidates
// everything into one oversized parcel (expensive under dim-weight pricing),
// while a billable-weight sorter splits into compact parcels that bill far less.
func TestCustomSorterChangesSelection(t *testing.T) {
	const divisor = 1000.0
	newBoxes := func() (cheap, roomy Box) {
		cheap = NewBox("cheap", 110, 110, 110, 100, 100, 100, 100, 100000)
		roomy = NewBox("roomy", 400, 400, 400, 500, 150, 150, 150, 100000)
		return
	}
	// nine light foam cubes; "roomy" can swallow all nine, "cheap" holds eight.
	foam := func() Item { return NewItem("foam", 50, 50, 50, 10, RotationBestFit) }

	// --- default objective ---
	cheap, roomy := newBoxes()
	def := NewPacker()
	def.AddBox(cheap)
	def.AddBox(roomy)
	def.AddItem(foam(), 9)
	defResult, err := def.Pack()
	if err != nil {
		t.Fatalf("default pack failed: %v", err)
	}
	if itemsPacked(defResult) != 9 {
		t.Fatalf("default packed %d items, want 9", itemsPacked(defResult))
	}
	if !usesBox(defResult, "roomy") {
		t.Fatalf("expected default objective to consolidate into the roomy box, got %v", boxRefs(defResult))
	}
	defBillable := totalBillable(defResult, divisor)

	// --- billable-weight objective ---
	cheap2, roomy2 := newBoxes()
	cost := NewPacker()
	cost.AddBox(cheap2)
	cost.AddBox(roomy2)
	cost.AddItem(foam(), 9)
	cost.SetPackedBoxSorter(minBillableWeightSorter{divisor: divisor})
	costResult, err := cost.Pack()
	if err != nil {
		t.Fatalf("cost pack failed: %v", err)
	}
	if itemsPacked(costResult) != 9 {
		t.Fatalf("cost packed %d items, want 9", itemsPacked(costResult))
	}
	if usesBox(costResult, "roomy") {
		t.Fatalf("expected billable-weight objective to avoid the roomy box, got %v", boxRefs(costResult))
	}
	costBillable := totalBillable(costResult, divisor)

	if costBillable >= defBillable {
		t.Fatalf("expected billable-weight objective to ship cheaper: cost=%v default=%v", costBillable, defBillable)
	}
	t.Logf("default: %v billable=%.0f | billable-sorter: %v billable=%.0f",
		boxRefs(defResult), defBillable, boxRefs(costResult), costBillable)
}

// TestNilSorterRestoresDefault confirms passing nil reverts to built-in order.
func TestNilSorterRestoresDefault(t *testing.T) {
	p := NewPacker()
	p.SetPackedBoxSorter(minBillableWeightSorter{divisor: 1000})
	p.SetPackedBoxSorter(nil)
	if _, ok := p.boxSorter.(defaultPackedBoxSorter); !ok {
		t.Fatalf("SetPackedBoxSorter(nil) left sorter as %T, want defaultPackedBoxSorter", p.boxSorter)
	}
}

func itemsPacked(boxes []*PackedBox) int {
	n := 0
	for _, b := range boxes {
		n += len(b.Items)
	}
	return n
}

func usesBox(boxes []*PackedBox, ref string) bool {
	for _, b := range boxes {
		if b.Box.Reference() == ref {
			return true
		}
	}
	return false
}

func boxRefs(boxes []*PackedBox) []string {
	refs := make([]string, len(boxes))
	for i, b := range boxes {
		refs[i] = b.Box.Reference()
	}
	return refs
}

func totalBillable(boxes []*PackedBox, divisor float64) float64 {
	total := 0.0
	for _, b := range boxes {
		total += BillableWeight(b, divisor)
	}
	return total
}

func ExampleVolumetricWeight() {
	// A 30 x 20 x 15 cm box, billed with a divisor of 5000 (cm -> kg).
	box := NewBox("parcel", 30, 20, 15, 0, 29, 19, 14, 100)
	fmt.Printf("%.1f\n", VolumetricWeight(box, 5000))
	// Output: 1.8
}
