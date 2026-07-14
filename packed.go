package boxpacker

import (
	"math"
	"sort"
)

// PackedItem is an item with a position and orientation inside a packed box.
//
// X/Y/Z are the coordinates of the corner of the item closest to the origin
// (the box corner). Width/Length/Depth are the dimensions of the item in its
// packed orientation.
type PackedItem struct {
	Item   Item `json:"item"`
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Z      int  `json:"z"`
	Width  int  `json:"width"`
	Length int  `json:"length"`
	Depth  int  `json:"depth"`
}

// Volume of the packed item.
func (p *PackedItem) Volume() int {
	return p.Width * p.Length * p.Depth
}

// packedItemList is a working list of packed items with cached totals.
type packedItemList struct {
	items  []*PackedItem
	weight int
	volume int
}

func (l *packedItemList) insert(item *PackedItem) {
	l.items = append(l.items, item)
	l.weight += item.Item.Weight()
	l.volume += item.Volume()
}

func (l *packedItemList) count() int {
	return len(l.items)
}

func (l *packedItemList) clone() *packedItemList {
	c := &packedItemList{
		items:  make([]*PackedItem, len(l.items)),
		weight: l.weight,
		volume: l.volume,
	}
	copy(c.items, l.items)
	return c
}

// PackedBox is a box with items packed into it.
type PackedBox struct {
	Box   Box           `json:"box"`
	Items []*PackedItem `json:"items"`

	itemWeight int
	usedVolume int
}

func newPackedBox(box Box, items *packedItemList) *PackedBox {
	// PHP PackedBox construction iterates PackedItemList to calculate weight,
	// which sorts the publicly observable item order by original volume and
	// weight. Apply that ordering eagerly for the Go slice representation.
	sortPackedItems(items.items)
	return &PackedBox{
		Box:        box,
		Items:      items.items,
		itemWeight: items.weight,
		usedVolume: items.volume,
	}
}

func (b *PackedBox) clone() *PackedBox {
	items := make([]*PackedItem, len(b.Items))
	copy(items, b.Items)
	return &PackedBox{Box: b.Box, Items: items, itemWeight: b.itemWeight, usedVolume: b.usedVolume}
}

// ItemWeight is the weight of the packed items, excluding the box itself.
func (b *PackedBox) ItemWeight() int {
	return b.itemWeight
}

// Weight is the total weight of the packed box, including the box itself.
func (b *PackedBox) Weight() int {
	return b.Box.EmptyWeight() + b.itemWeight
}

// RemainingWeight is the additional weight the box could hold.
func (b *PackedBox) RemainingWeight() int {
	return b.Box.MaxWeight() - b.Weight()
}

// InnerVolume of the box.
func (b *PackedBox) InnerVolume() int {
	return boxInnerVolume(b.Box)
}

// UsedVolume is the combined volume of the packed items.
func (b *PackedBox) UsedVolume() int {
	return b.usedVolume
}

// UnusedVolume is the empty space remaining inside the box.
func (b *PackedBox) UnusedVolume() int {
	return b.InnerVolume() - b.usedVolume
}

// VolumeUtilisation is the percentage (0-100) of the box volume in use,
// rounded to one decimal place to match PHP PackedBox semantics.
func (b *PackedBox) VolumeUtilisation() float64 {
	innerVolume := b.InnerVolume()
	if innerVolume == 0 {
		innerVolume = 1
	}
	utilisation := float64(b.usedVolume) / float64(innerVolume) * 100
	return math.Round(utilisation*10) / 10
}

// UsedWidth is the extent of the items along the box width.
func (b *PackedBox) UsedWidth() int {
	used := 0
	for _, item := range b.Items {
		used = maxInt(used, item.X+item.Width)
	}
	return used
}

// UsedLength is the extent of the items along the box length.
func (b *PackedBox) UsedLength() int {
	used := 0
	for _, item := range b.Items {
		used = maxInt(used, item.Y+item.Length)
	}
	return used
}

// UsedDepth is the extent of the items along the box depth.
func (b *PackedBox) UsedDepth() int {
	used := 0
	for _, item := range b.Items {
		used = maxInt(used, item.Z+item.Depth)
	}
	return used
}

// RemainingWidth inside the box for another item.
func (b *PackedBox) RemainingWidth() int {
	return b.Box.InnerWidth() - b.UsedWidth()
}

// RemainingLength inside the box for another item.
func (b *PackedBox) RemainingLength() int {
	return b.Box.InnerLength() - b.UsedLength()
}

// RemainingDepth inside the box for another item.
func (b *PackedBox) RemainingDepth() int {
	return b.Box.InnerDepth() - b.UsedDepth()
}

// comparePackedBoxes orders by item count (descending), then volume
// utilisation, then used volume, matching the PHP DefaultPackedBoxSorter.
func comparePackedBoxes(a, b *PackedBox) int {
	if len(a.Items) != len(b.Items) {
		if len(a.Items) > len(b.Items) {
			return -1
		}
		return 1
	}
	au, bu := a.VolumeUtilisation(), b.VolumeUtilisation()
	if au != bu {
		if au > bu {
			return -1
		}
		return 1
	}
	if a.usedVolume != b.usedVolume {
		if a.usedVolume > b.usedVolume {
			return -1
		}
		return 1
	}
	return 0
}

func sortPackedItems(items []*PackedItem) {
	sort.SliceStable(items, func(i, j int) bool {
		iv, jv := itemVolume(items[i].Item), itemVolume(items[j].Item)
		if iv != jv {
			return iv > jv
		}
		return items[i].Item.Weight() > items[j].Item.Weight()
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
