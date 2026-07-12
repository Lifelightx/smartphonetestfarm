// Package automation implements test script execution, scheduling, compiling, and locators.
//
// File: runner.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators.

package automation

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"protean-provider/internal/domain"
)

// StepResult stores metadata, execution metrics, and logs for an individual step run.
type StepResult struct {
	Index      int    `json:"index"`
	Action     string `json:"action"`
	Success    bool   `json:"success"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
	Screenshot []byte `json:"screenshot,omitempty"`
}

// Report holds the final execution metrics and details for a script run.
type Report struct {
	StartTime   time.Time    `json:"startTime"`
	EndTime     time.Time    `json:"endTime"`
	DurationMs  int64        `json:"durationMs"`
	TotalSteps  int          `json:"totalSteps"`
	PassedSteps int          `json:"passedSteps"`
	Success     bool         `json:"success"`
	Results     []StepResult `json:"results"`
}

// Runner manages the step-by-step execution of parsed YAML DSL scripts.
type Runner struct {
	driver domain.Driver
}

// NewRunner creates a new script execution Runner.
func NewRunner(driver domain.Driver) *Runner {
	return &Runner{
		driver: driver,
	}
}

// Run executes the given script and returns a detailed execution report.
// Implements retry strategies for non-assertion and non-wait step failures.
func (r *Runner) Run(ctx context.Context, script *Script) (*Report, error) {
	report := &Report{
		StartTime: time.Now(),
	}

	globalDelayMs := 0
	if script.Variables != nil {
		if val, ok := script.Variables["step_delay_ms"]; ok {
			if d, err := strconv.Atoi(val); err == nil {
				globalDelayMs = d
			}
		}
	}

	launchedPackages := make(map[string]bool)
	defer func() {
		for pkg := range launchedPackages {
			slog.Info("automation runner: terminating package at end of run", "package", pkg)
			_ = r.driver.Terminate(context.Background(), pkg)
		}
	}()

	success := true
	for i, step := range script.Steps {
		const maxRetries = 2
		var stepRes StepResult

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if ctx.Err() != nil {
				stepRes = StepResult{
					Index:   i,
					Action:  "cancelled",
					Success: false,
					Error:   ctx.Err().Error(),
				}
				break
			}

			stepRes = r.runStep(ctx, i, step, script.Variables)
			if stepRes.Success {
				if step.Launch != nil {
					launchedPackages[step.Launch.Package] = true
				}
				break
			}

			if stepRes.Action == "assert" || stepRes.Action == "wait" || attempt == maxRetries {
				break
			}

			slog.Warn("automation runner: step failed, retrying...", "step", i, "attempt", attempt+1, "err", stepRes.Error)

			select {
			case <-ctx.Done():
				break
			case <-time.After(1 * time.Second):
			}
		}

		report.Results = append(report.Results, stepRes)
		if stepRes.Success {
			report.PassedSteps++

			delayMs := step.DelayMs
			if delayMs <= 0 {
				delayMs = globalDelayMs
			}
			if delayMs > 0 {
				slog.Info("automation runner: delaying after step execution", "ms", delayMs)
				select {
				case <-ctx.Done():
				case <-time.After(time.Duration(delayMs) * time.Millisecond):
				}
			}
		} else {
			success = false
			slog.Warn("automation runner: step failed after retries, taking failure screenshot", "step", i, "err", stepRes.Error)
			screenshot, err := r.driver.Screenshot(ctx)
			if err == nil {
				report.Results[i].Screenshot = screenshot
			} else {
				slog.Error("automation runner: failed to capture screenshot on error", "err", err)
			}
			break
		}
	}

	report.EndTime = time.Now()
	report.DurationMs = report.EndTime.Sub(report.StartTime).Milliseconds()
	report.TotalSteps = len(script.Steps)
	report.Success = success

	return report, nil
}

// runStep dispatches execution to the appropriate driver function.
func (r *Runner) runStep(ctx context.Context, index int, step Step, vars map[string]string) StepResult {
	start := time.Now()
	var action string
	var err error

	if step.Click != nil || step.Input != nil || step.Wait != nil || step.Assert != nil {
		_ = r.waitForUIStabilization(ctx, 2*time.Second)
	}

	switch {
	case step.Launch != nil:
		action = "launch"
		err = r.driver.Launch(ctx, step.Launch.Package)
		if err == nil {
			_ = r.verifyPackageActive(ctx, step.Launch.Package, 3000)
		}

	case step.Terminate != nil:
		action = "terminate"
		err = r.driver.Terminate(ctx, step.Terminate.Package)

	case step.Input != nil:
		action = "input"
		err = r.handleInput(ctx, step.Input, vars)

	case step.Swipe != nil:
		action = "swipe"
		err = r.driver.Swipe(ctx, step.Swipe.StartX, step.Swipe.StartY, step.Swipe.EndX, step.Swipe.EndY, step.Swipe.DurationMs)

	case step.Click != nil:
		action = "click"
		err = r.handleClick(ctx, step.Click)

	case step.Wait != nil:
		action = "wait"
		err = r.handleWait(ctx, step.Wait)

	case step.Assert != nil:
		action = "assert"
		err = r.handleAssert(ctx, step.Assert)

	case step.If != nil:
		action = "if"
		conditionMet := r.evaluateCondition(ctx, step.If)
		var block []Step
		if conditionMet {
			block = step.Then
		} else {
			block = step.Else
		}
		for subIdx, subStep := range block {
			subRes := r.runStep(ctx, index*100+subIdx, subStep, vars)
			if !subRes.Success {
				err = fmt.Errorf("conditional block step failed at index %d: %s", subRes.Index, subRes.Error)
				break
			}
		}

	default:
		action = "unknown"
		err = fmt.Errorf("no action payload defined in step")
	}

	duration := time.Since(start).Milliseconds()
	res := StepResult{
		Index:      index,
		Action:     action,
		Success:    err == nil,
		DurationMs: duration,
	}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}
