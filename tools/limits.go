package tools

import (
	"log"
	"sync"
	"time"
)

type LimitQueeMap struct {
	sync.Mutex
	LimitQueue map[string][]int64
}

var LimitQueue = &LimitQueeMap{LimitQueue: make(map[string][]int64)}

func NewLimitQueue() {
	cleanLimitQueue()
}

func cleanLimitQueue() {
	go func() {
		for {
			log.Println("cleanLimitQueue start...")
			now := time.Now()
			LimitQueue.Lock()
			LimitQueue.LimitQueue = make(map[string][]int64)
			LimitQueue.Unlock()

			next := now.Add(24 * time.Hour)
			next = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, next.Location())
			<-time.After(next.Sub(now))
		}
	}()
}

// LimitFreqSingle implements a small in-process sliding-window limiter.
func LimitFreqSingle(queueName string, count uint, timeWindow int64) bool {
	if count == 0 {
		return false
	}
	currTime := time.Now().Unix()
	LimitQueue.Lock()
	defer LimitQueue.Unlock()

	q := LimitQueue.LimitQueue[queueName]
	if uint(len(q)) < count {
		LimitQueue.LimitQueue[queueName] = append(q, currTime)
		return true
	}
	if currTime-q[0] <= timeWindow {
		return false
	}
	LimitQueue.LimitQueue[queueName] = append(q[1:], currTime)
	return true
}
