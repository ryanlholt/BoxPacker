package boxpacker

import (
	"errors"
	"fmt"
	"testing"
)

// assertPackedBoxValid checks that all items are placed solely within the
// confines of the box, that no two items occupy the same physical space, and
// that the weight limit is respected.
func assertPackedBoxValid(t *testing.T, pb *PackedBox) {
	t.Helper()

	if pb.Weight() > pb.Box.MaxWeight() {
		t.Errorf("box %q over weight: %d > %d", pb.Box.Reference(), pb.Weight(), pb.Box.MaxWeight())
	}

	for i, item := range pb.Items {
		if item.X < 0 || item.X+item.Width > pb.Box.InnerWidth() {
			t.Errorf("item %d (%q) out of bounds on x: %d+%d vs width %d", i, item.Item.Description(), item.X, item.Width, pb.Box.InnerWidth())
		}
		if item.Y < 0 || item.Y+item.Length > pb.Box.InnerLength() {
			t.Errorf("item %d (%q) out of bounds on y: %d+%d vs length %d", i, item.Item.Description(), item.Y, item.Length, pb.Box.InnerLength())
		}
		if item.Z < 0 || item.Z+item.Depth > pb.Box.InnerDepth() {
			t.Errorf("item %d (%q) out of bounds on z: %d+%d vs depth %d", i, item.Item.Description(), item.Z, item.Depth, pb.Box.InnerDepth())
		}

		for j, other := range pb.Items[i+1:] {
			xOverlap := item.X < other.X+other.Width && other.X < item.X+item.Width
			yOverlap := item.Y < other.Y+other.Length && other.Y < item.Y+item.Length
			zOverlap := item.Z < other.Z+other.Depth && other.Z < item.Z+item.Depth
			if xOverlap && yOverlap && zOverlap {
				t.Errorf("items %d and %d overlap", i, i+1+j)
			}
		}
	}
}

func totalPackedItems(boxes []*PackedBox) int {
	total := 0
	for _, box := range boxes {
		total += len(box.Items)
	}
	return total
}

func TestPackSingleBox(t *testing.T) {
	packer := NewPacker()
	packer.AddBox(NewBox("small", 300, 300, 100, 100, 296, 296, 96, 1000))
	packer.AddItem(NewItem("flat thing", 250, 250, 20, 200, RotationBestFit), 2)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 1 {
		t.Fatalf("expected 1 box, got %d", len(packed))
	}
	if len(packed[0].Items) != 2 {
		t.Fatalf("expected 2 items in box, got %d", len(packed[0].Items))
	}
	assertPackedBoxValid(t, packed[0])
}

func TestWeightLimitForcesSplit(t *testing.T) {
	packer := NewPacker()
	// volume fits 8 items, weight only allows 4 per box
	packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1000))
	packer.AddItem(NewItem("heavy cube", 50, 50, 50, 200, RotationBestFit), 10)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 3 {
		t.Fatalf("expected 3 boxes (4+4+2), got %d", len(packed))
	}
	if got := totalPackedItems(packed); got != 10 {
		t.Fatalf("expected 10 items packed, got %d", got)
	}
	for _, pb := range packed {
		assertPackedBoxValid(t, pb)
	}
}

func TestMultipleBoxTypes(t *testing.T) {
	packer := NewPacker()
	packer.AddBox(NewBox("small", 60, 60, 60, 10, 50, 50, 50, 1000))
	packer.AddBox(NewBox("large", 250, 250, 120, 50, 240, 240, 110, 5000))
	packer.AddItem(NewItem("widget", 100, 100, 50, 100, RotationBestFit), 4)
	packer.AddItem(NewItem("gadget", 50, 50, 50, 50, RotationBestFit), 2)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := totalPackedItems(packed); got != 6 {
		t.Fatalf("expected 6 items packed, got %d", got)
	}
	if len(packed) != 1 {
		t.Fatalf("expected everything to fit the large box, got %d boxes", len(packed))
	}
	if packed[0].Box.Reference() != "large" {
		t.Fatalf("expected large box, got %q", packed[0].Box.Reference())
	}
	for _, pb := range packed {
		assertPackedBoxValid(t, pb)
	}
}

func TestUnpackableItemReturnsError(t *testing.T) {
	packer := NewPacker()
	packer.AddBox(NewBox("small", 60, 60, 60, 10, 50, 50, 50, 1000))
	packer.AddItem(NewItem("too big", 200, 200, 200, 100, RotationBestFit), 1)

	_, err := packer.Pack()
	var noBoxes *NoBoxesAvailableError
	if !errors.As(err, &noBoxes) {
		t.Fatalf("expected NoBoxesAvailableError, got %v", err)
	}
	if noBoxes.Item.Description() != "too big" {
		t.Fatalf("expected offending item in error, got %q", noBoxes.Item.Description())
	}
}

func TestAllowPartialResults(t *testing.T) {
	packer := NewPacker()
	packer.AllowPartialResults(true)
	packer.AddBox(NewBox("small", 60, 60, 60, 10, 50, 50, 50, 1000))
	packer.AddItem(NewItem("fits", 40, 40, 40, 100, RotationBestFit), 1)
	packer.AddItem(NewItem("too big", 200, 200, 200, 100, RotationBestFit), 1)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 1 || len(packed[0].Items) != 1 {
		t.Fatalf("expected 1 box with 1 item, got %d boxes", len(packed))
	}
	unpacked := packer.UnpackedItems()
	if len(unpacked) != 1 || unpacked[0].Description() != "too big" {
		t.Fatalf("expected 'too big' left unpacked, got %v", unpacked)
	}
}

func TestKeepFlatIsRespected(t *testing.T) {
	box := NewBox("shallow", 110, 110, 35, 10, 100, 100, 30, 1000)

	// On its side this would fit, but the item must stay flat
	flatOnly := NewPacker()
	flatOnly.AddBox(box)
	flatOnly.AddItem(NewItem("this way up", 30, 100, 100, 100, RotationKeepFlat), 1)
	if _, err := flatOnly.Pack(); err == nil {
		t.Fatal("expected KeepFlat item not to fit shallow box")
	}

	// The same shape with full rotation allowed fits fine
	bestFit := NewPacker()
	bestFit.AddBox(box)
	bestFit.AddItem(NewItem("any way up", 30, 100, 100, 100, RotationBestFit), 1)
	packed, err := bestFit.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 1 || packed[0].Items[0].Depth != 30 {
		t.Fatalf("expected item rotated to 30 depth, got %+v", packed[0].Items[0])
	}
}

func TestNoRotationIsRespected(t *testing.T) {
	box := NewBox("narrow", 110, 60, 60, 10, 100, 50, 50, 1000)

	never := NewPacker()
	never.AddBox(box)
	never.AddItem(NewItem("fixed orientation", 50, 100, 50, 100, RotationNever), 1)
	if _, err := never.Pack(); err == nil {
		t.Fatal("expected RotationNever item not to fit without rotation")
	}

	keepFlat := NewPacker()
	keepFlat.AddBox(box)
	keepFlat.AddItem(NewItem("rotatable", 50, 100, 50, 100, RotationKeepFlat), 1)
	if _, err := keepFlat.Pack(); err != nil {
		t.Fatalf("expected KeepFlat item to fit by rotating: %v", err)
	}
}

func TestLimitedSupplyBox(t *testing.T) {
	packer := NewPacker()
	packer.AllowPartialResults(true)
	// each box only fits one item, and there are only two boxes
	packer.AddBox(NewLimitedSupplyBox("scarce", 60, 60, 60, 10, 50, 50, 50, 1000, 2))
	packer.AddItem(NewItem("widget", 45, 45, 45, 100, RotationBestFit), 3)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 2 {
		t.Fatalf("expected 2 boxes, got %d", len(packed))
	}
	if len(packer.UnpackedItems()) != 1 {
		t.Fatalf("expected 1 unpacked item, got %d", len(packer.UnpackedItems()))
	}
}

func TestQuantityShortCircuitEquivalence(t *testing.T) {
	pack := func(shortCircuit bool) []*PackedBox {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(shortCircuit)
		packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1000))
		packer.AddBox(NewBox("crate", 510, 510, 110, 500, 500, 500, 100, 10000))
		packer.AddItem(NewItem("heavy cube", 50, 50, 50, 200, RotationBestFit), 37)
		packed, err := packer.Pack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return packed
	}

	withSC := pack(true)
	withoutSC := pack(false)

	if len(withSC) != len(withoutSC) {
		t.Fatalf("box counts differ: %d with short-circuit, %d without", len(withSC), len(withoutSC))
	}
	for i := range withSC {
		if withSC[i].Box.Reference() != withoutSC[i].Box.Reference() {
			t.Errorf("box %d type differs: %q vs %q", i, withSC[i].Box.Reference(), withoutSC[i].Box.Reference())
		}
		if len(withSC[i].Items) != len(withoutSC[i].Items) {
			t.Errorf("box %d item count differs: %d vs %d", i, len(withSC[i].Items), len(withoutSC[i].Items))
		}
	}
	if got := totalPackedItems(withSC); got != 37 {
		t.Fatalf("expected 37 items packed, got %d", got)
	}
}

func TestLargeQuantityCompletesQuickly(t *testing.T) {
	packer := NewPacker()
	// weight allows 4 items per box, so 100k items = 25k boxes
	packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1000))
	packer.AddItem(NewItem("heavy cube", 50, 50, 50, 200, RotationBestFit), 100_000)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := totalPackedItems(packed); got != 100_000 {
		t.Fatalf("expected 100000 items packed, got %d", got)
	}
	if len(packed) != 25_000 {
		t.Fatalf("expected 25000 boxes, got %d", len(packed))
	}
	// replicated boxes must still carry valid placements
	assertPackedBoxValid(t, packed[0])
	assertPackedBoxValid(t, packed[len(packed)/2])
	assertPackedBoxValid(t, packed[len(packed)-1])
}

func TestMixedItemsPackValidly(t *testing.T) {
	packer := NewPacker()
	packer.AddBox(NewBox("medium", 250, 250, 120, 50, 240, 240, 110, 5000))
	packer.AddBox(NewBox("large", 350, 350, 250, 100, 340, 340, 240, 10000))
	packer.AddItem(NewItem("book", 200, 130, 30, 500, RotationKeepFlat), 5)
	packer.AddItem(NewItem("mug", 100, 100, 100, 300, RotationNever), 3)
	packer.AddItem(NewItem("poster tube", 300, 60, 60, 200, RotationBestFit), 2)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := totalPackedItems(packed); got != 10 {
		t.Fatalf("expected 10 items packed, got %d", got)
	}
	for _, pb := range packed {
		assertPackedBoxValid(t, pb)
	}
}

func TestPackedBoxAccessors(t *testing.T) {
	packer := NewPacker()
	packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1000))
	packer.AddItem(NewItem("half-width slab", 50, 100, 100, 200, RotationBestFit), 1)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pb := packed[0]
	if pb.ItemWeight() != 200 {
		t.Errorf("ItemWeight = %d, want 200", pb.ItemWeight())
	}
	if pb.Weight() != 300 {
		t.Errorf("Weight = %d, want 300", pb.Weight())
	}
	if pb.UsedVolume() != 500_000 {
		t.Errorf("UsedVolume = %d, want 500000", pb.UsedVolume())
	}
	if util := pb.VolumeUtilisation(); util != 50 {
		t.Errorf("VolumeUtilisation = %f, want 50", util)
	}
	if pb.RemainingWeight() != 700 {
		t.Errorf("RemainingWeight = %d, want 700", pb.RemainingWeight())
	}
}

// TestManyBoxTypesParallelEvaluation exercises the parallel box-evaluation
// path with enough box types to fan out, and checks the smallest adequate box
// still wins deterministically.
func TestManyBoxTypesParallelEvaluation(t *testing.T) {
	makePacker := func() *Packer {
		packer := NewPacker()
		for size := 50; size <= 500; size += 25 { // 19 box types
			packer.AddBox(NewBox(fmt.Sprintf("cube-%d", size), size+10, size+10, size+10, 100, size, size, size, 100_000))
		}
		packer.AddItem(NewItem("widget", 100, 100, 50, 100, RotationBestFit), 4)
		packer.AddItem(NewItem("gadget", 50, 50, 50, 50, RotationBestFit), 6)
		return packer
	}

	first, err := makePacker().Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := totalPackedItems(first); got != 10 {
		t.Fatalf("expected 10 items packed, got %d", got)
	}
	for _, pb := range first {
		assertPackedBoxValid(t, pb)
	}

	// repeated runs must choose identical boxes
	for run := 0; run < 5; run++ {
		again, err := makePacker().Pack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(again) != len(first) {
			t.Fatalf("run %d: box count %d, want %d", run, len(again), len(first))
		}
		for i := range again {
			if again[i].Box.Reference() != first[i].Box.Reference() || len(again[i].Items) != len(first[i].Items) {
				t.Fatalf("run %d: box %d is %q/%d items, want %q/%d items",
					run, i, again[i].Box.Reference(), len(again[i].Items), first[i].Box.Reference(), len(first[i].Items))
			}
		}
	}
}

func BenchmarkLargeQuantity(b *testing.B) {
	for i := 0; i < b.N; i++ {
		packer := NewPacker()
		packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1000))
		packer.AddItem(NewItem("heavy cube", 50, 50, 50, 200, RotationBestFit), 100_000)
		if _, err := packer.Pack(); err != nil {
			b.Fatal(err)
		}
	}
}
