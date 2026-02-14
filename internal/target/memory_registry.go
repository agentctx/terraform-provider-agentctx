package target

import "sync"

// memoryRegistry holds global MemoryTarget instances that persist across
// provider re-initializations within a single test run. The
// terraform-plugin-testing framework re-creates the provider between test
// steps, so a global registry is needed to keep MemoryTarget state alive.
var (
	memoryRegistryMu sync.Mutex
	memoryRegistry   = make(map[string]*MemoryTarget)
)

// GetOrCreateMemoryTarget returns an existing MemoryTarget with the given
// name, or creates a new one if it does not already exist.
func GetOrCreateMemoryTarget(name string) *MemoryTarget {
	memoryRegistryMu.Lock()
	defer memoryRegistryMu.Unlock()

	if t, ok := memoryRegistry[name]; ok {
		return t
	}

	t := NewMemoryTarget(name)
	memoryRegistry[name] = t
	return t
}

// ResetMemoryTargets clears the global MemoryTarget registry. Call this in
// test cleanup to ensure isolation between test cases.
func ResetMemoryTargets() {
	memoryRegistryMu.Lock()
	defer memoryRegistryMu.Unlock()

	memoryRegistry = make(map[string]*MemoryTarget)
}
