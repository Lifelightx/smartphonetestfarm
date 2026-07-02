package automation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSchedulerRunParallel(t *testing.T) {
	scheduler := NewScheduler()

	script := &Script{
		Steps: []Step{},
	}

	var mu sync.Mutex
	execCounts := make(map[string]int)

	executeFn := func(ctx context.Context, serial string, s *Script) (*Report, error) {
		mu.Lock()
		execCounts[serial]++
		mu.Unlock()

		time.Sleep(10 * time.Millisecond) // Simulate some work
		return &Report{Success: true}, nil
	}

	tasks := []Task{
		{Serial: "device1", Script: script, ExecuteFn: executeFn},
		{Serial: "device2", Script: script, ExecuteFn: executeFn},
	}

	results := scheduler.RunParallel(context.Background(), tasks)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, res := range results {
		if res.Err != nil {
			t.Errorf("task on %s failed unexpectedly: %v", res.Serial, res.Err)
		}
		if res.Report == nil || !res.Report.Success {
			t.Errorf("task on %s did not return a successful report", res.Serial)
		}
	}

	mu.Lock()
	if execCounts["device1"] != 1 || execCounts["device2"] != 1 {
		t.Errorf("unexpected execute counts: %+v", execCounts)
	}
	mu.Unlock()
}

func TestSchedulerMutualExclusion(t *testing.T) {
	scheduler := NewScheduler()

	script := &Script{}
	startChan := make(chan struct{})
	finishChan := make(chan struct{})

	executeFn := func(ctx context.Context, serial string, s *Script) (*Report, error) {
		close(startChan)
		<-finishChan // Wait until test signals to finish
		return &Report{Success: true}, nil
	}

	// Task 1 will start and block on finishChan
	t1 := Task{Serial: "device1", Script: script, ExecuteFn: executeFn}

	// Task 2 uses same serial and will try to run concurrently
	t2 := Task{
		Serial: "device1",
		Script: script,
		ExecuteFn: func(ctx context.Context, serial string, s *Script) (*Report, error) {
			return nil, errors.New("should not be called")
		},
	}

	var wg sync.WaitGroup
	var res1, res2 TaskResult

	wg.Add(1)
	go func() {
		defer wg.Done()
		r := scheduler.RunParallel(context.Background(), []Task{t1})
		res1 = r[0]
	}()

	// Wait for Task 1 to actually start and acquire lock
	<-startChan

	// Trigger Task 2 on the same serial, it should return immediately with an error
	r2 := scheduler.RunParallel(context.Background(), []Task{t2})
	res2 = r2[0]

	if res2.Err == nil {
		t.Error("expected Task 2 to fail with busy error, got nil")
	} else if res2.Err.Error() != "device device1 is busy running another automation script" {
		t.Errorf("unexpected error for Task 2: %v", res2.Err)
	}

	// Now let Task 1 finish
	close(finishChan)
	wg.Wait()

	if res1.Err != nil {
		t.Errorf("Task 1 failed unexpectedly: %v", res1.Err)
	}
}
