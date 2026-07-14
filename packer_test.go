package boxpacker

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

// canonicalPacking describes physical packing while ignoring the order in
// which boxes and packed items are returned.
func canonicalPacking(boxes []*PackedBox) []string {
	boxSignatures := make([]string, 0, len(boxes))
	for _, box := range boxes {
		itemSignatures := make([]string, 0, len(box.Items))
		for _, item := range box.Items {
			itemSignatures = append(itemSignatures, fmt.Sprintf(
				"%s:%d:%d:%d:%d:%d:%d",
				item.Item.Description(), item.X, item.Y, item.Z,
				item.Width, item.Length, item.Depth,
			))
		}
		sort.Strings(itemSignatures)
		boxSignatures = append(boxSignatures, box.Box.Reference()+"#"+strings.Join(itemSignatures, "|"))
	}
	sort.Strings(boxSignatures)
	return boxSignatures
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

// TestPHPFirstItemOrientationParity pins a fixture where exhaustively trying
// every orientation of the first item finds a denser result than the PHP 3.x
// heuristic. Compatibility requires the first item to use the same bounded
// lookahead orientation sorter as every item that follows it.
func TestPHPFirstItemOrientationParity(t *testing.T) {
	packer := NewPacker()
	packer.SetQuantityShortCircuit(false)
	packer.AddBox(NewBox("small", 107, 118, 125, 0, 107, 118, 125, 1_000_000))
	packer.AddBox(NewBox("large", 170, 160, 150, 0, 170, 160, 150, 1_000_000))
	packer.AddItem(NewItem("SKU0", 45, 92, 54, 322, RotationBestFit), 9)
	packer.AddItem(NewItem("SKU1", 55, 70, 41, 376, RotationKeepFlat), 4)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 2 {
		t.Fatalf("expected the PHP-compatible 2-box result, got %d boxes", len(packed))
	}

	type makeup struct{ sku0, sku1 int }
	got := make(map[string]makeup, len(packed))
	for _, packedBox := range packed {
		assertPackedBoxValid(t, packedBox)
		contents := makeup{}
		for _, packedItem := range packedBox.Items {
			switch packedItem.Item.Description() {
			case "SKU0":
				contents.sku0++
			case "SKU1":
				contents.sku1++
			default:
				t.Fatalf("unexpected item %q", packedItem.Item.Description())
			}
		}
		got[packedBox.Box.Reference()] = contents
	}

	if want := (makeup{sku0: 9, sku1: 2}); got["large"] != want {
		t.Errorf("large box makeup = %+v, want %+v", got["large"], want)
	}
	if want := (makeup{sku1: 2}); got["small"] != want {
		t.Errorf("small box makeup = %+v, want %+v", got["small"], want)
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

func TestQuantityShortCircuitIsOptIn(t *testing.T) {
	if NewPacker().quantityShortCircuit {
		t.Fatal("NewPacker enabled the quantity short-circuit by default")
	}
}

func TestQuantityCappingPreservesLookaheadWindow(t *testing.T) {
	pack := func(shortCircuit bool) []*PackedBox {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(shortCircuit)
		packer.AddBox(NewBox("Box", 154, 85, 149, 22, 154, 85, 149, 5772))
		// Weight limits this box to four items, below the orientation lookahead
		// depth, while seven copies are available to the uncapped evaluation.
		packer.AddItem(NewItem("Big", 56, 40, 70, 1300, RotationBestFit), 7)

		packed, err := packer.Pack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return packed
	}

	withoutShortCircuit := canonicalPacking(pack(false))
	withShortCircuit := canonicalPacking(pack(true))
	if !slices.Equal(withShortCircuit, withoutShortCircuit) {
		t.Fatalf("quantity capping changed physical packing:\nwithout: %v\nwith:    %v", withoutShortCircuit, withShortCircuit)
	}
}

// TestQuantityReplicationPreservesDepletedPoolOrientations ports the PHP
// fork's regression where cloning a full box after the bounded pool has begun
// shrinking changes the physical orientation of a later box.
func TestQuantityReplicationPreservesDepletedPoolOrientations(t *testing.T) {
	pack := func(shortCircuit bool) []*PackedBox {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(shortCircuit)
		packer.AddBox(NewBox("Box", 68, 74, 40, 0, 68, 74, 40, 4_280))
		packer.AddItem(NewItem("Widget", 33, 30, 22, 1_151, RotationBestFit), 11)

		packed, err := packer.Pack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return packed
	}

	withoutShortCircuit := canonicalPacking(pack(false))
	withShortCircuit := canonicalPacking(pack(true))
	if !slices.Equal(withShortCircuit, withoutShortCircuit) {
		t.Fatalf("replication changed depleted-pool packing:\nwithout: %v\nwith:    %v", withoutShortCircuit, withShortCircuit)
	}
}

func TestQuantityReplicationHonoursLimitedBoxSupply(t *testing.T) {
	type result struct {
		packed   []*PackedBox
		unpacked int
	}
	pack := func(shortCircuit bool) result {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(shortCircuit)
		packer.AllowPartialResults(true)
		packer.AddBox(NewLimitedSupplyBox("scarce", 60, 60, 60, 0, 50, 50, 50, 1_000, 3))
		packer.AddItem(NewItem("widget", 45, 45, 45, 100, RotationBestFit), 30)

		packed, err := packer.Pack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return result{packed: packed, unpacked: len(packer.UnpackedItems())}
	}

	withoutShortCircuit := pack(false)
	withShortCircuit := pack(true)
	if len(withShortCircuit.packed) != 3 {
		t.Fatalf("packed %d scarce boxes, want exactly the 3 available", len(withShortCircuit.packed))
	}
	if withShortCircuit.unpacked != 27 {
		t.Fatalf("left %d items unpacked, want 27", withShortCircuit.unpacked)
	}
	if !slices.Equal(canonicalPacking(withShortCircuit.packed), canonicalPacking(withoutShortCircuit.packed)) {
		t.Fatalf("limited-supply packing differs with replication enabled")
	}
}

func TestQuantityReplicationEvaluationCountIsQuantityIndependent(t *testing.T) {
	uniformEvaluations := func(quantity int) int64 {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(true)
		var evaluations atomic.Int64
		packer.boxEvaluationObserver = func(Box) { evaluations.Add(1) }
		packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1_000))
		packer.AddItem(NewItem("heavy cube", 50, 50, 50, 200, RotationBestFit), quantity)
		if _, err := packer.Pack(); err != nil {
			t.Fatalf("uniform quantity %d: unexpected error: %v", quantity, err)
		}
		return evaluations.Load()
	}

	mixedEvaluations := func(quantity int) int64 {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(true)
		var evaluations atomic.Int64
		packer.boxEvaluationObserver = func(Box) { evaluations.Add(1) }
		packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1_000_000))
		packer.AddItem(NewItem("A slab", 100, 100, 60, 100, RotationBestFit), quantity)
		packer.AddItem(NewItem("B slab", 100, 100, 40, 80, RotationBestFit), quantity)
		if _, err := packer.Pack(); err != nil {
			t.Fatalf("mixed quantity %d: unexpected error: %v", quantity, err)
		}
		return evaluations.Load()
	}

	if small, large := uniformEvaluations(100), uniformEvaluations(10_000); small != large {
		t.Errorf("uniform evaluations scale with quantity: 100 items = %d, 10000 items = %d", small, large)
	} else if large > int64(orientationLookaheadDepth+2) {
		t.Errorf("uniform packing required %d real evaluations, want at most %d", large, orientationLookaheadDepth+2)
	}
	if small, large := mixedEvaluations(100), mixedEvaluations(1_000); small != large {
		t.Errorf("mixed evaluations scale with quantity: 100 pairs = %d, 1000 pairs = %d", small, large)
	} else if large > int64(orientationLookaheadDepth+2) {
		t.Errorf("mixed packing required %d real evaluations, want at most %d", large, orientationLookaheadDepth+2)
	}
}

func TestQuantityReplicationIsDisabledForCustomSorter(t *testing.T) {
	type result struct {
		packed      []*PackedBox
		evaluations int64
	}
	pack := func(shortCircuit bool) result {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(shortCircuit)
		packer.SetPackedBoxSorter(PackedBoxSorterFunc(func(_, _ *PackedBox) int { return 0 }))
		var evaluations atomic.Int64
		packer.boxEvaluationObserver = func(Box) { evaluations.Add(1) }
		smallBox := NewBox("Small", 1, 1, 9, 0, 1, 1, 9, 10)
		packer.AddBox(smallBox)
		packer.AddBox(NewBox("Large", 1, 1, 10, 0, 1, 1, 10, 10))
		packer.AddItem(NewItem("Item", 1, 1, 1, 10, RotationBestFit), 13)
		if shortCircuit {
			bounded := packer.itemsForBoxEvaluation(smallBox, packer.items.signatureCounts())
			if got := bounded.count(); got != 1+orientationLookaheadDepth {
				t.Fatalf("custom sorter received %d bounded items, want %d", got, 1+orientationLookaheadDepth)
			}
		}

		packed, err := packer.Pack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return result{packed: packed, evaluations: evaluations.Load()}
	}

	withoutShortCircuit := pack(false)
	withShortCircuit := pack(true)
	if !slices.Equal(canonicalPacking(withShortCircuit.packed), canonicalPacking(withoutShortCircuit.packed)) {
		t.Fatalf("custom-sorter packing changed when the short-circuit was enabled")
	}
	if withShortCircuit.evaluations != 26 {
		t.Fatalf("custom sorter used %d candidate evaluations, want 26 real evaluations with no replication", withShortCircuit.evaluations)
	}
}

func TestQuantityReplicationStopsAtPreferredBoxBoundary(t *testing.T) {
	type result struct {
		packed      []*PackedBox
		evaluations int64
	}
	pack := func(shortCircuit bool) result {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(shortCircuit)
		var evaluations atomic.Int64
		packer.boxEvaluationObserver = func(Box) { evaluations.Add(1) }
		packer.AddBox(NewBox("BoxA", 10, 12, 247, 0, 10, 12, 247, 20))
		packer.AddBox(NewBox("BoxB", 30, 20, 50, 0, 30, 20, 50, 20))
		packer.AddItem(NewItem("Widget", 10, 10, 10, 10, RotationBestFit), 40)

		packed, err := packer.Pack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return result{packed: packed, evaluations: evaluations.Load()}
	}

	withoutShortCircuit := pack(false)
	withShortCircuit := pack(true)
	if !slices.Equal(canonicalPacking(withShortCircuit.packed), canonicalPacking(withoutShortCircuit.packed)) {
		t.Fatalf("packing changed across the preferred-box boundary")
	}
	if len(withShortCircuit.packed) != 20 {
		t.Fatalf("packed %d boxes, want 20", len(withShortCircuit.packed))
	}
	// Both box types are in stock throughout, so 14 candidate evaluations are
	// seven real packing iterations. Crossing the boundary would use fewer.
	if withShortCircuit.evaluations != 14 {
		t.Fatalf("used %d candidate evaluations, want 14 so the boundary iteration is solved normally", withShortCircuit.evaluations)
	}
}

func TestQuantityReplicationDoesNotReplacePreferenceBoundaryIteration(t *testing.T) {
	packer := NewPacker()
	packer.SetQuantityShortCircuit(true)
	var evaluations atomic.Int64
	packer.boxEvaluationObserver = func(Box) { evaluations.Add(1) }
	packer.AddBox(NewBox("Box", 1, 1, 10, 0, 1, 1, 10, 10))
	packer.AddItem(NewItem("Item", 1, 1, 1, 10, RotationBestFit), 11)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 11 {
		t.Fatalf("packed %d boxes, want 11", len(packed))
	}
	if got := evaluations.Load(); got != 10 {
		t.Fatalf("used %d real evaluations, want 10 so equality at the boundary is not replicated", got)
	}
}

func TestQuantityReplicationZeroVolumeTemplateHasNoPreferenceLimit(t *testing.T) {
	packer := NewPacker()
	packer.SetQuantityShortCircuit(true)
	packer.AllowPartialResults(true)
	var evaluations atomic.Int64
	packer.boxEvaluationObserver = func(Box) { evaluations.Add(1) }
	packer.AddBox(NewBox("Box", 5, 5, 5, 0, 5, 5, 5, 10))
	packer.AddItem(NewItem("ZeroVol", 0, 1, 1, 5, RotationBestFit), 1_000)
	packer.AddItem(NewItem("TooLarge", 6, 6, 6, 1, RotationBestFit), 1)

	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packed) != 500 {
		t.Fatalf("packed %d boxes, want 500", len(packed))
	}
	if unpacked := packer.UnpackedItems(); len(unpacked) != 1 || unpacked[0].Description() != "TooLarge" {
		t.Fatalf("unexpected unpacked items: %v", unpacked)
	}
	if got := evaluations.Load(); got > 6 {
		t.Fatalf("zero-volume template required %d evaluations, want at most 6", got)
	}
}

func TestItemsForBoxEvaluationUsesBoundedHeadroom(t *testing.T) {
	box := NewBox("bounded", 10, 10, 10, 0, 10, 10, 10, 100)
	packer := NewPacker()
	packer.SetQuantityShortCircuit(true)
	packer.AddItem(NewItem("zero-volume", 0, 1, 1, 0, RotationBestFit), 2_000)
	packer.AddItem(NewItem("zero-weight", 10, 10, 10, 0, RotationBestFit), 20)
	packer.AddItem(NewItem("overweight", 1, 1, 1, 101, RotationBestFit), 20)

	capped := packer.itemsForBoxEvaluation(box, packer.items.signatureCounts())
	counts := capped.signatureCounts()

	tests := []struct {
		item Item
		want int
	}{
		// Zero volume is treated as unit volume: 1000 capacity + 8 headroom.
		{NewItem("zero-volume", 0, 1, 1, 0, RotationBestFit), 1_008},
		// Zero weight leaves volume as the binding limit: 1 + 8 headroom.
		{NewItem("zero-weight", 10, 10, 10, 0, RotationBestFit), 9},
		// An overweight item has zero physical capacity but retains the window.
		{NewItem("overweight", 1, 1, 1, 101, RotationBestFit), 8},
	}
	for _, test := range tests {
		if got := counts[signatureOf(test.item)]; got != test.want {
			t.Errorf("%s capped count = %d, want %d", test.item.Description(), got, test.want)
		}
	}
}

func TestItemsForBoxEvaluationReturnsOriginalWhenNoCapIsNeeded(t *testing.T) {
	box := NewBox("roomy", 10, 10, 10, 0, 10, 10, 10, 100)
	packer := NewPacker()
	packer.SetQuantityShortCircuit(true)
	packer.AddItem(NewItem("small", 5, 5, 5, 1, RotationBestFit), 2)

	if got := packer.itemsForBoxEvaluation(box, packer.items.signatureCounts()); got != packer.items {
		t.Fatal("expected the original item list when no signature needs capping")
	}
}

func TestItemsForBoxEvaluationBoundIsIndependentOfQuantity(t *testing.T) {
	box := NewBox("bounded", 10, 10, 10, 0, 10, 10, 10, 1_000_000)
	item := NewItem("unit-cube", 5, 5, 5, 1, RotationBestFit)
	evaluationCount := func(quantity int) int {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(true)
		packer.AddItem(item, quantity)
		bounded := packer.itemsForBoxEvaluation(box, packer.items.signatureCounts())
		return bounded.count()
	}

	want := 8 + orientationLookaheadDepth // physical capacity plus headroom
	if got := evaluationCount(100); got != want {
		t.Fatalf("100-item evaluation count = %d, want %d", got, want)
	}
	if got := evaluationCount(100_000); got != want {
		t.Fatalf("100000-item evaluation count = %d, want %d", got, want)
	}
}

func TestLargeQuantityCompletesQuickly(t *testing.T) {
	packer := NewPacker()
	packer.SetQuantityShortCircuit(true)
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

// TestLargeMixedQuantityCompletesQuickly packs large quantities of several
// distinct item types. Without the per-signature work-bounding and multiset
// short-circuit this re-solves the entire (huge) pool on every iteration and
// takes seconds; with them it must finish near-instantly.
func TestLargeMixedQuantityCompletesQuickly(t *testing.T) {
	packer := NewPacker()
	packer.SetQuantityShortCircuit(true)
	packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 100_000))
	packer.AddItem(NewItem("A", 50, 50, 50, 50, RotationBestFit), 4_000)
	packer.AddItem(NewItem("B", 40, 40, 40, 40, RotationBestFit), 4_000)
	packer.AddItem(NewItem("C", 30, 30, 30, 30, RotationBestFit), 4_000)

	start := time.Now()
	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("mixed pack took %s, expected the short-circuit to keep it fast", elapsed)
	}
	if got := totalPackedItems(packed); got != 12_000 {
		t.Fatalf("expected 12000 items packed, got %d", got)
	}
	assertPackedBoxValid(t, packed[0])
	assertPackedBoxValid(t, packed[len(packed)/2])
	assertPackedBoxValid(t, packed[len(packed)-1])
}

// TestMixedBoxShortCircuit exercises the short-circuit on a winning box made up
// of more than one item type. Each box is packed with one A slab and one B slab
// stacked, and that exact mix repeats, so the multiset short-circuit must
// replicate it.
func TestMixedBoxShortCircuit(t *testing.T) {
	pack := func(shortCircuit bool) []*PackedBox {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(shortCircuit)
		packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1_000_000))
		// A fills the bottom 60 of depth, B the remaining 40, so each box holds
		// exactly one of each.
		packer.AddItem(NewItem("A slab", 100, 100, 60, 100, RotationBestFit), 500)
		packer.AddItem(NewItem("B slab", 100, 100, 40, 80, RotationBestFit), 500)
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
	if got := totalPackedItems(withSC); got != 1_000 {
		t.Fatalf("expected 1000 items packed, got %d", got)
	}
	// every box must actually carry the A+B mix that was replicated
	for i, pb := range withSC {
		assertPackedBoxValid(t, pb)
		var a, b int
		for _, item := range pb.Items {
			switch item.Item.Description() {
			case "A slab":
				a++
			case "B slab":
				b++
			}
		}
		if a != 1 || b != 1 {
			t.Fatalf("box %d holds %d A and %d B, want 1 of each", i, a, b)
		}
	}
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
		packer.SetQuantityShortCircuit(true)
		packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 1000))
		packer.AddItem(NewItem("heavy cube", 50, 50, 50, 200, RotationBestFit), 100_000)
		if _, err := packer.Pack(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeMixedQuantity(b *testing.B) {
	for i := 0; i < b.N; i++ {
		packer := NewPacker()
		packer.SetQuantityShortCircuit(true)
		packer.AddBox(NewBox("cube", 110, 110, 110, 100, 100, 100, 100, 100_000))
		packer.AddItem(NewItem("A", 50, 50, 50, 50, RotationBestFit), 3_000)
		packer.AddItem(NewItem("B", 40, 40, 40, 40, RotationBestFit), 3_000)
		packer.AddItem(NewItem("C", 30, 30, 30, 30, RotationBestFit), 3_000)
		if _, err := packer.Pack(); err != nil {
			b.Fatal(err)
		}
	}
}
