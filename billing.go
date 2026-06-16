package boxpacker

// VolumetricWeight returns the dimensional ("dim") weight of a box: its outer
// volume divided by a carrier's dimensional divisor. Most carriers bill the
// greater of actual and dimensional weight, so dim weight penalises large light
// parcels.
//
// The box dimensions and the divisor must be in consistent units, and the
// result is in the same weight unit as Item.Weight. For example, with outer
// dimensions in centimetres a divisor of 5000 yields kilograms; with inches a
// divisor of 139 yields pounds. Because this library stores dimensions as
// integers, choose units (e.g. mm, or cm) and a matching divisor accordingly.
//
// The divisor must be positive; a non-positive divisor yields 0.
func VolumetricWeight(box Box, divisor float64) float64 {
	if divisor <= 0 {
		return 0
	}
	outerVolume := box.OuterWidth() * box.OuterLength() * box.OuterDepth()
	return float64(outerVolume) / divisor
}

// BillableWeight returns the weight a carrier would charge for a packed box:
// the greater of its actual gross weight (box plus contents) and its volumetric
// weight. The divisor has the same meaning and unit requirements as in
// VolumetricWeight.
//
// This is a convenience for building cost-aware PackedBoxSorter implementations;
// it does not apply any carrier-specific rounding, which callers can layer on
// top.
func BillableWeight(box *PackedBox, divisor float64) float64 {
	volumetric := VolumetricWeight(box.Box, divisor)
	actual := float64(box.Weight())
	if volumetric > actual {
		return volumetric
	}
	return actual
}
