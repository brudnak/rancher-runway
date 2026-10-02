package imagelookup

import (
	"sync"
	"testing"
)

func TestOptionsInitializeBeforeConcurrentUse(t *testing.T) {
	// Preferred-image resolution shares one custom-keychain service across workers.
	service := NewWithOptions(Options{Keychain: imageLookupAnonymousTestKeychain{}})
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if service.CommandRunner() == nil {
				t.Error("missing command runner")
			}
		}()
	}
	workers.Wait()
}
