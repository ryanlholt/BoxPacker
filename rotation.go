package boxpacker

// Rotation describes the orientations an item is allowed to be placed in.
type Rotation int

const (
	// RotationNever means the item must be placed in its defined orientation only.
	RotationNever Rotation = 1

	// RotationKeepFlat means the item can be turned sideways 90°, but cannot
	// be placed on its side, e.g. fragile "this way up" items.
	RotationKeepFlat Rotation = 2

	// RotationBestFit means the item has no handling restrictions and can be
	// placed in any orientation.
	RotationBestFit Rotation = 6
)

func (r Rotation) String() string {
	switch r {
	case RotationNever:
		return "Never"
	case RotationKeepFlat:
		return "KeepFlat"
	case RotationBestFit:
		return "BestFit"
	default:
		return "Unknown"
	}
}
