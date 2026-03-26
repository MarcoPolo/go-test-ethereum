package main

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Minimal repro: synctest can't advance time when a goroutine waits on a mutex
// held by another goroutine that is durably blocked (sleeping).
func TestSynctestMutexSleep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex

		// Goroutine A: holds the mutex and sleeps
		go func() {
			mu.Lock()
			time.Sleep(1 * time.Second) // durable block
			mu.Unlock()
		}()

		// Give goroutine A a moment to acquire the lock
		time.Sleep(1 * time.Millisecond)

		// Goroutine B (main): tries to acquire the mutex
		// This is a non-durable block — will synctest advance time?
		mu.Lock()
		mu.Unlock()

		t.Log("mutex acquired — time advanced correctly")
	})
}
