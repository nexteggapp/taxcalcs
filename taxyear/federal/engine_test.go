package federal

import (
	"runtime"
	"testing"
	"time"

	rules "github.com/nexteggapp/taxcalcs/returns/rules/federal"
)

// TestEngineLoadCost logs what loading the rules engine costs natively:
// the time to parse, typecheck and initialise the rules, and the heap
// they keep. Compute loads it once per process.
func TestEngineLoadCost(t *testing.T) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	eng, err := rules.LoadEngine()
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("rules engine: loaded in %v, %.1f MiB allocated while loading, %.1f MiB kept on the heap",
		took, float64(allocated)/(1<<20), float64(int64(after.HeapAlloc)-int64(before.HeapAlloc))/(1<<20))
	runtime.KeepAlive(eng)
}
