package boxpacker

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
)

type phpParityCorpus struct {
	Source struct {
		Repository string `json:"repository"`
		Branch     string `json:"branch"`
		Commit     string `json:"commit"`
	} `json:"source"`
	Scenarios []phpParityScenario `json:"scenarios"`
}

type phpParityScenario struct {
	Name                    string                    `json:"name"`
	MaxBoxesToBalanceWeight int                       `json:"max_boxes_to_balance_weight"`
	Boxes                   []phpParityBoxDefinition  `json:"boxes"`
	Items                   []phpParityItemDefinition `json:"items"`
	ExpectedBoxes           []phpParityPackedBox      `json:"expected_boxes"`
}

type phpParityBoxDefinition struct {
	Reference   string `json:"reference"`
	OuterWidth  int    `json:"outer_width"`
	OuterLength int    `json:"outer_length"`
	OuterDepth  int    `json:"outer_depth"`
	EmptyWeight int    `json:"empty_weight"`
	InnerWidth  int    `json:"inner_width"`
	InnerLength int    `json:"inner_length"`
	InnerDepth  int    `json:"inner_depth"`
	MaxWeight   int    `json:"max_weight"`
	Quantity    *int   `json:"quantity"`
}

type phpParityItemDefinition struct {
	Description string `json:"description"`
	Width       int    `json:"width"`
	Length      int    `json:"length"`
	Depth       int    `json:"depth"`
	Weight      int    `json:"weight"`
	Rotation    string `json:"rotation"`
	Quantity    int    `json:"quantity"`
}

type phpParityPackedBox struct {
	Reference         string                `json:"reference"`
	VolumeUtilisation float64               `json:"volume_utilisation"`
	Items             []phpParityPackedItem `json:"items"`
}

type phpParityPackedItem struct {
	Description string `json:"description"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Z           int    `json:"z"`
	Width       int    `json:"width"`
	Length      int    `json:"length"`
	Depth       int    `json:"depth"`
}

func TestPHPParityGoldenCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/php_parity_corpus.json")
	if err != nil {
		t.Fatalf("read PHP parity corpus: %v", err)
	}
	var corpus phpParityCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("decode PHP parity corpus: %v", err)
	}
	if corpus.Source.Branch != "feature/quantity-short-circuit" {
		t.Fatalf("unexpected PHP corpus source branch %q", corpus.Source.Branch)
	}
	if corpus.Source.Commit != "e0aa3a969b5fe650db11a90b5acfed948018de69" {
		t.Fatalf("unexpected PHP corpus source commit %q", corpus.Source.Commit)
	}

	for _, scenario := range corpus.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			for _, shortCircuit := range []bool{false, true} {
				t.Run(fmt.Sprintf("short-circuit=%t", shortCircuit), func(t *testing.T) {
					packer := NewPacker()
					packer.SetQuantityShortCircuit(shortCircuit)
					packer.SetMaxBoxesToBalanceWeight(scenario.MaxBoxesToBalanceWeight)
					for _, definition := range scenario.Boxes {
						arguments := []int{
							definition.OuterWidth, definition.OuterLength, definition.OuterDepth,
							definition.EmptyWeight,
							definition.InnerWidth, definition.InnerLength, definition.InnerDepth,
							definition.MaxWeight,
						}
						if definition.Quantity == nil {
							packer.AddBox(NewBox(definition.Reference, arguments[0], arguments[1], arguments[2], arguments[3], arguments[4], arguments[5], arguments[6], arguments[7]))
						} else {
							packer.AddBox(NewLimitedSupplyBox(definition.Reference, arguments[0], arguments[1], arguments[2], arguments[3], arguments[4], arguments[5], arguments[6], arguments[7], *definition.Quantity))
						}
					}
					for _, definition := range scenario.Items {
						packer.AddItem(NewItem(
							definition.Description,
							definition.Width,
							definition.Length,
							definition.Depth,
							definition.Weight,
							phpParityRotation(t, definition.Rotation),
						), definition.Quantity)
					}

					packed, err := packer.Pack()
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					for _, box := range packed {
						assertPackedBoxValid(t, box)
					}
					got := phpParityResult(packed)
					if !reflect.DeepEqual(got, scenario.ExpectedBoxes) {
						gotJSON, _ := json.MarshalIndent(got, "", "  ")
						wantJSON, _ := json.MarshalIndent(scenario.ExpectedBoxes, "", "  ")
						t.Fatalf("ordered PHP parity result differs:\ngot:\n%s\nwant:\n%s", gotJSON, wantJSON)
					}
				})
			}
		})
	}
}

func phpParityRotation(t *testing.T, value string) Rotation {
	t.Helper()
	switch value {
	case "best_fit":
		return RotationBestFit
	case "keep_flat":
		return RotationKeepFlat
	default:
		t.Fatalf("unknown corpus rotation %q", value)
		return RotationNever
	}
}

func phpParityResult(boxes []*PackedBox) []phpParityPackedBox {
	result := make([]phpParityPackedBox, len(boxes))
	for boxIndex, box := range boxes {
		result[boxIndex] = phpParityPackedBox{
			Reference:         box.Box.Reference(),
			VolumeUtilisation: box.VolumeUtilisation(),
			Items:             make([]phpParityPackedItem, len(box.Items)),
		}
		for itemIndex, item := range box.Items {
			result[boxIndex].Items[itemIndex] = phpParityPackedItem{
				Description: item.Item.Description(),
				X:           item.X, Y: item.Y, Z: item.Z,
				Width: item.Width, Length: item.Length, Depth: item.Depth,
			}
		}
	}
	return result
}

type parityLCG struct{ state uint64 }

func (r *parityLCG) next() uint64 {
	r.state = (1_664_525*r.state + 1_013_904_223) & 0xffffffff
	return r.state
}

func (r *parityLCG) between(low, high int) int {
	return low + int(r.next()%uint64(high-low+1))
}

type parityBoxDefinition struct {
	reference                    string
	width, length, depth, weight int
}

type parityItemDefinition struct {
	description                            string
	width, length, depth, weight, quantity int
	rotation                               Rotation
}

func TestQuantityShortCircuitSeededPhysicalParity(t *testing.T) {
	random := &parityLCG{state: 20_260_711}
	for scenario := 0; scenario < 100; scenario++ {
		width := random.between(60, 160)
		length := random.between(60, 160)
		depth := random.between(40, 150)
		boxes := []parityBoxDefinition{
			{"S", width, length, depth, random.between(3_000, 12_000)},
			{"L", width + random.between(10, 70), length + random.between(10, 70), depth + random.between(5, 50), random.between(8_000, 25_000)},
		}
		skuCount := random.between(1, 4)
		items := make([]parityItemDefinition, 0, skuCount)
		for sku := 0; sku < skuCount; sku++ {
			rotation := RotationBestFit
			if random.between(0, 4) == 0 {
				rotation = RotationKeepFlat
			}
			items = append(items, parityItemDefinition{
				description: fmt.Sprintf("I%d", sku),
				width:       random.between(15, width),
				length:      random.between(15, length),
				depth:       random.between(10, depth),
				weight:      random.between(100, 1_800),
				quantity:    random.between(12, 55),
				rotation:    rotation,
			})
		}

		withoutShortCircuit := packParityScenario(t, boxes, items, false)
		withShortCircuit := packParityScenario(t, boxes, items, true)
		if !slices.Equal(withShortCircuit, withoutShortCircuit) {
			t.Fatalf("scenario %d physical packing differs with short-circuit enabled:\nboxes=%+v\nitems=%+v\nwithout=%v\nwith=%v", scenario, boxes, items, withoutShortCircuit, withShortCircuit)
		}
	}
}

func packParityScenario(t *testing.T, boxes []parityBoxDefinition, items []parityItemDefinition, shortCircuit bool) []string {
	t.Helper()
	packer := NewPacker()
	packer.SetQuantityShortCircuit(shortCircuit)
	for _, box := range boxes {
		packer.AddBox(NewBox(box.reference, box.width, box.length, box.depth, 0, box.width, box.length, box.depth, box.weight))
	}
	for _, item := range items {
		packer.AddItem(NewItem(item.description, item.width, item.length, item.depth, item.weight, item.rotation), item.quantity)
	}
	packed, err := packer.Pack()
	if err != nil {
		t.Fatalf("short-circuit %t: unexpected error: %v", shortCircuit, err)
	}
	return canonicalPacking(packed)
}
