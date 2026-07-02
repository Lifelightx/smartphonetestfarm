package automation

import (
	"context"
	"fmt"
	"sync"
)

// Task represents an automation execution task on a single device.
type Task struct {
	Serial    string
	Script    *Script
	ExecuteFn func(ctx context.Context, serial string, script *Script) (*Report, error)
}

// TaskResult contains the output of a scheduled task.
type TaskResult struct {
	Serial string
	Report *Report
	Err    error
}

// Scheduler orchestrates parallel test execution across multiple devices.
type Scheduler struct {
	mu         sync.Mutex
	activeRuns map[string]bool // serial -> active status
}

// NewScheduler creates a new thread-safe Scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{
		activeRuns: make(map[string]bool),
	}
}

// RunParallel runs tasks concurrently, enforcing that only one task runs per device serial at a time.
func (s *Scheduler) RunParallel(ctx context.Context, tasks []Task) []TaskResult {
	var wg sync.WaitGroup
	results := make([]TaskResult, len(tasks))

	for i, task := range tasks {
		wg.Add(1)
		go func(index int, t Task) {
			defer wg.Done()

			// Lock device serial to ensure only one test runs per device serial at a time
			s.mu.Lock()
			if s.activeRuns[t.Serial] {
				s.mu.Unlock()
				results[index] = TaskResult{
					Serial: t.Serial,
					Err:    fmt.Errorf("device %s is busy running another automation script", t.Serial),
				}
				return
			}
			s.activeRuns[t.Serial] = true
			s.mu.Unlock()

			defer func() {
				s.mu.Lock()
				delete(s.activeRuns, t.Serial)
				s.mu.Unlock()
			}()

			report, err := t.ExecuteFn(ctx, t.Serial, t.Script)
			results[index] = TaskResult{
				Serial: t.Serial,
				Report: report,
				Err:    err,
			}
		}(i, task)
	}

	wg.Wait()
	return results
}
