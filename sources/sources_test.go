package sources

import (
	"testing"
)

func TestCatalogChecks(t *testing.T) {
	catalog, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) == 0 {
		t.Fatal("the catalog is empty")
	}
	for _, err := range Check(catalog) {
		t.Error(err)
	}
}
