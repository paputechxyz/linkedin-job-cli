package fx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// approx returns true when a and b are within tol (relative) of each other.
func approx(a, b, tol float64) bool {
	if b == 0 {
		return a == 0
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d/tol <= 1 // |a-b| <= tol
}

// writeCache writes a today-dated rate cache so Convert is deterministic and
// never hits the network.
func writeCache(t *testing.T, rates map[string]float64) {
	t.Helper()
	dir := t.TempDir()
	CacheFile = filepath.Join(dir, "fx_cache.json")
	if rates[rateBase] == 0 {
		rates[rateBase] = 1.0
	}
	rc := rateCache{Date: today(), Base: rateBase, Rates: rates, FetchedAt: "now"}
	data, _ := json.Marshal(rc)
	if err := os.WriteFile(CacheFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	cached = rateCache{}
	loaded = false
	mu.Unlock()
}

func TestConvert_SameCurrencyNoOp(t *testing.T) {
	writeCache(t, map[string]float64{"CAD": 1.36})
	got, err := Convert(160000, "cad", "CAD")
	if err != nil || got != 160000 {
		t.Fatalf("got=%v err=%v, want 160000 nil", got, err)
	}
}

func TestConvert_CrossRate(t *testing.T) {
	// 1 USD = 1.36 CAD. 200000 CAD -> USD = 200000 / 1.36 ≈ 147058.82.
	writeCache(t, map[string]float64{"CAD": 1.36})
	got, err := Convert(200000, "CAD", "USD")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	want := 200000 / 1.36
	if !approx(got, want, 0.01) {
		t.Fatalf("CAD->USD got=%v want=%v", got, want)
	}
	// Inverse: 160000 USD -> CAD = 160000 * 1.36 = 217600.
	got, err = Convert(160000, "USD", "CAD")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !approx(got, 217600, 0.5) {
		t.Fatalf("USD->CAD got=%v want 217600", got)
	}
}

func TestConvert_UnsupportedCurrency(t *testing.T) {
	writeCache(t, map[string]float64{"CAD": 1.36})
	if _, err := Convert(100, "USD", "XYZ"); err == nil {
		t.Fatal("expected error for unsupported target")
	}
	if _, err := Convert(100, "XYZ", "USD"); err == nil {
		t.Fatal("expected error for unsupported source")
	}
}

func TestConvert_EmptyCurrency(t *testing.T) {
	writeCache(t, map[string]float64{"CAD": 1.36})
	if _, err := Convert(100, "", "USD"); err == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize(" cad "); got != "CAD" {
		t.Errorf("Normalize=%q want CAD", got)
	}
}

func TestSupported(t *testing.T) {
	writeCache(t, map[string]float64{"CAD": 1.36})
	if !Supported("CAD") {
		t.Error("CAD should be supported")
	}
	if Supported("XYZ") {
		t.Error("XYZ should not be supported")
	}
	if Supported("") {
		t.Error("empty should not be supported")
	}
}
