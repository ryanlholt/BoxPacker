package boxpacker

import "math"

// Box is a container that items are packed into.
//
// Implementations must be comparable with == (use a pointer type). Methods may
// be called concurrently during packing and must be safe for concurrent reads.
type Box interface {
	// Reference returns the box type reference, e.g. SKU or description.
	Reference() string

	// OuterWidth of the box, including the packaging itself.
	OuterWidth() int

	// OuterLength of the box, including the packaging itself.
	OuterLength() int

	// OuterDepth of the box, including the packaging itself.
	OuterDepth() int

	// EmptyWeight of the box when empty.
	EmptyWeight() int

	// InnerWidth available for packing items.
	InnerWidth() int

	// InnerLength available for packing items.
	InnerLength() int

	// InnerDepth available for packing items.
	InnerDepth() int

	// MaxWeight the packaging can hold, including its own empty weight.
	MaxWeight() int
}

// LimitedSupplyBox is a Box that there are only a finite number of available.
type LimitedSupplyBox interface {
	Box

	// QuantityAvailable returns how many of this box are available to pack into.
	QuantityAvailable() int
}

// StandardBox is a simple immutable Box implementation.
type StandardBox struct {
	reference                           string
	outerWidth, outerLength, outerDepth int
	emptyWeight                         int
	innerWidth, innerLength, innerDepth int
	maxWeight                           int
}

// NewBox creates a StandardBox.
func NewBox(reference string, outerWidth, outerLength, outerDepth, emptyWeight, innerWidth, innerLength, innerDepth, maxWeight int) *StandardBox {
	return &StandardBox{
		reference:   reference,
		outerWidth:  outerWidth,
		outerLength: outerLength,
		outerDepth:  outerDepth,
		emptyWeight: emptyWeight,
		innerWidth:  innerWidth,
		innerLength: innerLength,
		innerDepth:  innerDepth,
		maxWeight:   maxWeight,
	}
}

func (b *StandardBox) Reference() string { return b.reference }
func (b *StandardBox) OuterWidth() int   { return b.outerWidth }
func (b *StandardBox) OuterLength() int  { return b.outerLength }
func (b *StandardBox) OuterDepth() int   { return b.outerDepth }
func (b *StandardBox) EmptyWeight() int  { return b.emptyWeight }
func (b *StandardBox) InnerWidth() int   { return b.innerWidth }
func (b *StandardBox) InnerLength() int  { return b.innerLength }
func (b *StandardBox) InnerDepth() int   { return b.innerDepth }
func (b *StandardBox) MaxWeight() int    { return b.maxWeight }

// StandardLimitedSupplyBox is a StandardBox with a finite quantity available.
type StandardLimitedSupplyBox struct {
	StandardBox
	quantityAvailable int
}

// NewLimitedSupplyBox creates a StandardLimitedSupplyBox.
func NewLimitedSupplyBox(reference string, outerWidth, outerLength, outerDepth, emptyWeight, innerWidth, innerLength, innerDepth, maxWeight, quantityAvailable int) *StandardLimitedSupplyBox {
	return &StandardLimitedSupplyBox{
		StandardBox:       *NewBox(reference, outerWidth, outerLength, outerDepth, emptyWeight, innerWidth, innerLength, innerDepth, maxWeight),
		quantityAvailable: quantityAvailable,
	}
}

func (b *StandardLimitedSupplyBox) QuantityAvailable() int { return b.quantityAvailable }

func boxInnerVolume(b Box) int {
	return b.InnerWidth() * b.InnerLength() * b.InnerDepth()
}

// compareBoxes orders by inner volume (ascending, smallest first), then empty
// weight, then net weight capacity, matching the PHP DefaultBoxSorter.
func compareBoxes(a, b Box) int {
	av, bv := boxInnerVolume(a), boxInnerVolume(b)
	if av != bv {
		if av < bv {
			return -1
		}
		return 1
	}
	if a.EmptyWeight() != b.EmptyWeight() {
		if a.EmptyWeight() < b.EmptyWeight() {
			return -1
		}
		return 1
	}
	aCapacity := a.MaxWeight() - a.EmptyWeight()
	bCapacity := b.MaxWeight() - b.EmptyWeight()
	if aCapacity != bCapacity {
		if aCapacity < bCapacity {
			return -1
		}
		return 1
	}
	return 0
}

// workingVolume is a virtual box used for lookahead calculations.
type workingVolume struct {
	width, length, depth int
}

func (w *workingVolume) Reference() string { return "Working Volume" }
func (w *workingVolume) OuterWidth() int   { return w.width }
func (w *workingVolume) OuterLength() int  { return w.length }
func (w *workingVolume) OuterDepth() int   { return w.depth }
func (w *workingVolume) EmptyWeight() int  { return 0 }
func (w *workingVolume) InnerWidth() int   { return w.width }
func (w *workingVolume) InnerLength() int  { return w.length }
func (w *workingVolume) InnerDepth() int   { return w.depth }
func (w *workingVolume) MaxWeight() int    { return math.MaxInt }
