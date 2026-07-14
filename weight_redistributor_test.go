package boxpacker

import (
	"slices"
	"sort"
	"testing"
)

func TestWeightRedistributionMatchesPHPUnitFixture(t *testing.T) {
	packer := NewPacker()
	packer.AddBox(NewBox("Box", 1, 1, 3, 0, 1, 1, 3, 3))
	packer.AddItem(NewItem("Item", 1, 1, 1, 1, RotationBestFit), 4)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"Box#Item:0:0:0:1:1:1|Item:0:0:1:1:1:1",
		"Box#Item:0:0:0:1:1:1|Item:0:0:1:1:1:1",
	}
	if got := canonicalPacking(packed); !slices.Equal(got, want) {
		t.Fatalf("redistributed packing differs from PHP:\ngot:  %v\nwant: %v", got, want)
	}
}

func TestWeightRedistributionMatchesPHPMixedFixture(t *testing.T) {
	packer := NewPacker()
	packer.AddBox(NewBox("Box", 370, 375, 60, 140, 364, 374, 40, 3_000))
	packer.AddItem(NewItem("Item 1", 230, 330, 6, 320, RotationKeepFlat), 2)
	packer.AddItem(NewItem("Item 2", 210, 297, 8, 300, RotationKeepFlat), 4)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"Box#Item 1:0:0:0:330:230:6|Item 2:0:0:14:297:210:8|Item 2:0:0:6:297:210:8",
		"Box#Item 1:0:0:0:330:230:6|Item 2:0:0:14:297:210:8|Item 2:0:0:6:297:210:8",
	}
	if got := canonicalPacking(packed); !slices.Equal(got, want) {
		t.Fatalf("redistributed packing differs from PHP:\ngot:  %v\nwant: %v", got, want)
	}
	if packedWeightVariance(packed) != 0 {
		t.Fatalf("weight variance = %v, want 0", packedWeightVariance(packed))
	}
}

func TestWeightRedistributionMatchesPHPCumulativeTransferFixture(t *testing.T) {
	packer := NewPacker()
	packer.AddBox(NewBox("Small", 81, 124, 99, 0, 81, 124, 99, 1_000_000))
	packer.AddBox(NewBox("Large", 170, 160, 150, 0, 170, 160, 150, 1_000_000))
	packer.AddItem(NewItem("SKU0", 67, 30, 72, 208, RotationKeepFlat), 9)
	packer.AddItem(NewItem("SKU1", 95, 95, 11, 278, RotationBestFit), 3)
	packer.AddItem(NewItem("SKU2", 83, 80, 63, 116, RotationKeepFlat), 4)
	packer.AddItem(NewItem("SKU3", 85, 36, 59, 166, RotationBestFit), 5)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 2 {
		t.Fatalf("packed %d boxes, want 2", len(packed))
	}

	makeups := make(map[int]map[string]int, len(packed))
	for _, box := range packed {
		counts := make(map[string]int)
		for _, item := range box.Items {
			counts[item.Item.Description()]++
		}
		makeups[box.ItemWeight()] = counts
	}
	want := map[int]map[string]int{
		1_944: {"SKU0": 5, "SKU1": 2, "SKU2": 3},
		2_056: {"SKU0": 4, "SKU1": 1, "SKU2": 1, "SKU3": 5},
	}
	if !makeupCountsEqual(makeups, want) {
		t.Fatalf("redistributed makeup = %v, want %v", makeups, want)
	}
}

func TestWeightRedistributionCanBeDisabled(t *testing.T) {
	packer := NewPacker()
	if got := packer.MaxBoxesToBalanceWeight(); got != 12 {
		t.Fatalf("default maximum = %d, want 12", got)
	}
	packer.SetMaxBoxesToBalanceWeight(0)
	if got := packer.MaxBoxesToBalanceWeight(); got != 0 {
		t.Fatalf("configured maximum = %d, want 0", got)
	}
	packer.AddBox(NewBox("Box", 1, 1, 3, 0, 1, 1, 3, 3))
	packer.AddItem(NewItem("Item", 1, 1, 1, 1, RotationBestFit), 4)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := packedItemCounts(packed); !slices.Equal(got, []int{1, 3}) {
		t.Fatalf("item counts with redistribution disabled = %v, want [1 3]", got)
	}
}

func TestWeightRedistributionHonoursConfiguredMaximum(t *testing.T) {
	pack := func(maxBoxes int) []*PackedBox {
		packer := NewPacker()
		packer.SetMaxBoxesToBalanceWeight(maxBoxes)
		packer.AddBox(NewBox("Box", 1, 1, 3, 0, 1, 1, 3, 3))
		packer.AddItem(NewItem("Item", 1, 1, 1, 1, RotationBestFit), 37)
		packed, err := packer.Pack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return packed
	}

	if got := packedItemCounts(pack(12)); !slices.Equal(got, []int{1, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3}) {
		t.Fatalf("13-box result was unexpectedly redistributed: %v", got)
	}
	if got := packedItemCounts(pack(13)); !slices.Equal(got, []int{2, 2, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3}) {
		t.Fatalf("configured 13-box result was not redistributed: %v", got)
	}
}

func TestWeightRedistributionPreservesLimitedBoxSupply(t *testing.T) {
	box := NewLimitedSupplyBox("limited", 1, 1, 3, 0, 1, 1, 3, 3, 2)
	packer := NewPacker()
	packer.AddBox(box)
	packer.AddItem(NewItem("Item", 1, 1, 1, 1, RotationBestFit), 4)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 2 {
		t.Fatalf("packed %d boxes, want 2", len(packed))
	}
	if got := packer.boxQuantities[box]; got != 0 {
		t.Fatalf("remaining limited-box quantity = %d, want 0", got)
	}
	for _, packedBox := range packed {
		assertPackedBoxValid(t, packedBox)
	}
}

func TestWeightRedistributionPreservesActiveSorter(t *testing.T) {
	packer := NewPacker()
	packer.SetPackedBoxSorter(PackedBoxSorterFunc(func(a, b *PackedBox) int {
		return a.ItemWeight() - b.ItemWeight()
	}))
	packer.AddBox(NewBox("Box", 1, 1, 3, 0, 1, 1, 3, 3))
	packer.AddItem(NewItem("Item", 1, 1, 1, 1, RotationBestFit), 7)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	weights := make([]int, len(packed))
	for i, box := range packed {
		weights[i] = box.ItemWeight()
	}
	if !slices.Equal(weights, []int{2, 2, 3}) {
		t.Fatalf("active sorter order = %v, want [2 2 3]", weights)
	}
}

func packedItemCounts(boxes []*PackedBox) []int {
	counts := make([]int, len(boxes))
	for i, box := range boxes {
		counts[i] = len(box.Items)
	}
	sort.Ints(counts)
	return counts
}

func packedWeightVariance(boxes []*PackedBox) float64 {
	mean := 0.0
	for _, box := range boxes {
		mean += float64(box.Weight())
	}
	mean /= float64(len(boxes))

	variance := 0.0
	for _, box := range boxes {
		difference := float64(box.Weight()) - mean
		variance += difference * difference
	}
	return variance / float64(len(boxes))
}

func makeupCountsEqual(got, want map[int]map[string]int) bool {
	if len(got) != len(want) {
		return false
	}
	for weight, wantCounts := range want {
		gotCounts, ok := got[weight]
		if !ok || len(gotCounts) != len(wantCounts) {
			return false
		}
		for description, count := range wantCounts {
			if gotCounts[description] != count {
				return false
			}
		}
	}
	return true
}
