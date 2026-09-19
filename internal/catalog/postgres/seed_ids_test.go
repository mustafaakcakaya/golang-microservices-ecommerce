package postgres

import "testing"

// The sample product ids are pinned as literals rather than recomputed.
//
// Recomputing them with the same function would pass whatever that function
// returned, so a change to the namespace, the names or the derivation would go
// unnoticed - and it would silently re-key every seeded product for anything
// that stores these ids, such as API examples, saved requests, or another
// service that keeps its own copy of the catalog.
func TestSeedIDsAreStable(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"iPhone X":    "aed705de-0368-5fba-be0a-0594eede2efa",
		"Samsung 10":  "07b14e76-aad3-51f9-8b7c-a2428b9e757e",
		"Huawei Plus": "f3afd807-4f59-5baf-a891-9f9e6f818725",
		"Xiaomi Mi 9": "f022c00d-b3d1-5829-b889-2ba81e9d6fff",
	}

	catalog := sampleCatalog()
	if len(catalog) != len(want) {
		t.Fatalf("sample catalog has %d products, want %d", len(catalog), len(want))
	}

	for _, product := range catalog {
		expected, known := want[product.Name]
		if !known {
			t.Errorf("unexpected sample product %q", product.Name)
			continue
		}
		if got := product.ID.String(); got != expected {
			t.Errorf("%s: id = %s, want %s", product.Name, got, expected)
		}
	}
}
