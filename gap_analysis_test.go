package boxpacker_test

import "testing"

// A completed search of a restricted pattern pool is not a global proof: this
// pool contains only single-item patterns although two items fit in one box.
func TestRestrictedPatternCompletionDoesNotProveGlobalOptimum(t *testing.T) {
	catalog := benchCatalog{
		boxes: []benchBox{{"ten", 10, 1, 1, 0, 10, 1, 1, 100}},
		skus:  []benchSKU{{name: "item", w: 5, l: 1, d: 1, weight: 1, qty: 2}},
	}
	patterns := []pattern{{boxIdx: 0, counts: []int{1}, outerVol: 10, items: 1}}
	count, _, complete := branchAndBound(catalog, patterns, []int{2}, 2, 20)
	if !complete || count != 2 {
		t.Fatalf("restricted result %d, complete %t", count, complete)
	}
	if bound := lowerBound(catalog, []int{2}); bound != 1 {
		t.Fatalf("independent bound %d, want 1", bound)
	}
	// The independent feasible construction is two width-five items at x=0,5.
	if count == 1 {
		t.Fatal("restricted patterns unexpectedly represented the global solution")
	}
}
