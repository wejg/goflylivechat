package tools

import (
	"sync"
	"testing"
)

func TestLimitFreqSingleConcurrentWindow(t *testing.T) {
	LimitQueue.Lock()
	LimitQueue.LimitQueue = make(map[string][]int64)
	LimitQueue.Unlock()

	var wg sync.WaitGroup
	allowed := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if LimitFreqSingle("test", 1, 60) {
				allowed <- struct{}{}
			}
		}()
	}
	wg.Wait()
	if got := len(allowed); got != 1 {
		t.Fatalf("expected one request in the window, got %d", got)
	}
}
