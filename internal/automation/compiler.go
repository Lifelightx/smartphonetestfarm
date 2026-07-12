// Package automation implements test script execution, scheduling, compiling, and locators.
//
// File: compiler.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators.

package automation

import (
	"fmt"
	"log/slog"
	"strings"
)

// CompileScript takes raw recorded events and produces a smart automation Script
// with ranked locators, confidence scores, and anchor context.
// This is where ALL intelligence lives.
func CompileScript(events []RawEvent) *Script {
	if len(events) == 0 {
		return &Script{Steps: []Step{}}
	}

	var steps []Step
	var launchPackage string

	for _, event := range events {
		switch event.Type {
		case "launch":
			launchPackage = event.Package
			steps = append(steps, Step{
				Launch: &LaunchParams{
					Package: event.Package,
				},
			})
			// Automatically append wait and assert configurations after launch
			steps = append(steps, Step{
				Wait: &WaitParams{
					Class:     "android.widget.TextView",
					Condition: "visible",
					TimeoutMs: 3000,
				},
			})
			steps = append(steps, Step{
				Assert: &AssertParams{
					Text:      inferAssertText(event.Package),
					Condition: "contains",
				},
			})

		case "click":
			step := compileClickEvent(event)
			steps = append(steps, step)

		case "input":
			steps = append(steps, Step{
				Input: &InputParams{
					Text: event.Text,
				},
			})

		case "swipe":
			steps = append(steps, Step{
				Swipe: &SwipeParams{
					StartX:     event.TouchX,
					StartY:     event.TouchY,
					EndX:       event.EndX,
					EndY:       event.EndY,
					DurationMs: event.DurationMs,
				},
			})
		}
	}

	// Optimize: merge consecutive single-char inputs, filter duplicate clicks
	optimized := optimizeSteps(steps)

	if launchPackage != "" {
		optimized = append(optimized, Step{
			Terminate: &TerminateParams{
				Package: launchPackage,
			},
		})
		optimized = append(optimized, Step{
			Wait: &WaitParams{
				Class:     "android.widget.TextView",
				Condition: "hidden",
				TimeoutMs: 3000,
			},
		})
	}

	return &Script{
		Steps: optimized,
	}
}

// compileClickEvent is the core intelligence for a single click event.
// It scores nodes, picks the best target, generates ranked locators, and extracts anchors.
func compileClickEvent(event RawEvent) Step {
	x := event.TouchX
	y := event.TouchY

	// If no UI XML was captured, fall back to coordinates only
	if event.UIXML == "" {
		slog.Info("compiler: no XML available, using coordinates only", "x", x, "y", y)
		return Step{
			Click: &ClickParams{
				Locators: []Locator{
					{Strategy: "coordinates", Value: "", Confidence: 10, X: x, Y: y},
				},
				X: &x,
				Y: &y,
			},
		}
	}

	root, err := ParseXMLTree(event.UIXML)
	if err != nil {
		slog.Warn("compiler: failed to parse UI XML tree, using coordinates only", "err", err)
		return Step{
			Click: &ClickParams{
				Locators: []Locator{
					{Strategy: "coordinates", Value: "", Confidence: 10, X: x, Y: y},
				},
				X: &x,
				Y: &y,
			},
		}
	}

	bestEl := FindBestElementAt(root, x, y, event.ScreenWidth, event.ScreenHeight)
	if bestEl == nil {
		slog.Warn("compiler: no node found at coordinates, using coordinates only", "x", x, "y", y)
		return Step{
			Click: &ClickParams{
				Locators: []Locator{
					{Strategy: "coordinates", Value: "", Confidence: 10, X: x, Y: y},
				},
				X: &x,
				Y: &y,
			},
		}
	}

	var locators []Locator
	var anchor *AnchorContext
	if bestEl.XMLRef != nil {
		locators = GenerateLocators(event.UIXML, bestEl.XMLRef, x, y)
		anchor = ExtractAnchor(event.UIXML, bestEl.XMLRef)
	} else {
		locators = []Locator{{Strategy: "coordinates", Value: fmt.Sprintf("%.6f,%.6f", x, y), Confidence: 10, X: x, Y: y}}
	}

	slog.Info("compiler: compiled click event",
		"node_class", bestEl.Class,
		"node_text", bestEl.Text,
		"node_resourceId", bestEl.ResourceID,
		"node_score", ScoreActionableNode(bestEl, event.ScreenWidth, event.ScreenHeight),
		"locator_count", len(locators),
		"has_anchor", anchor != nil,
		"x", x, "y", y,
	)

	clickParams := &ClickParams{
		Target:   bestEl,
		Locators: locators,
		Anchor:   anchor,
		X:        &x,
		Y:        &y,
	}

	// Also populate legacy fields for backward compatibility
	if bestEl.ResourceID != "" {
		clickParams.ResourceID = bestEl.ResourceID
	}
	if bestEl.ContentDesc != "" {
		clickParams.ContentDesc = bestEl.ContentDesc
	}
	if bestEl.Text != "" {
		clickParams.Text = bestEl.Text
	}
	if bestEl.Class != "" {
		clickParams.Class = bestEl.Class
	}

	return Step{Click: clickParams}
}

// optimizeSteps merges consecutive single-character input steps and filters duplicate clicks.
func optimizeSteps(steps []Step) []Step {
	if len(steps) == 0 {
		return steps
	}
	optimized := make([]Step, 0, len(steps))
	var pendingInput *InputParams

	for _, step := range steps {
		if step.Input != nil {
			if pendingInput == nil {
				pendingInput = &InputParams{
					Text:        step.Input.Text,
					Target:      step.Input.Target,
					ResourceID:  step.Input.ResourceID,
					ContentDesc: step.Input.ContentDesc,
					Class:       step.Input.Class,
					XPath:       step.Input.XPath,
					X:           step.Input.X,
					Y:           step.Input.Y,
				}
				// If the previous step was a click on an EditText, associate its target with the input step
				if len(optimized) > 0 {
					prev := optimized[len(optimized)-1]
					if prev.Click != nil && prev.Click.Target != nil && prev.Click.Target.Class == "android.widget.EditText" {
						pendingInput.Target = prev.Click.Target
						pendingInput.ResourceID = prev.Click.ResourceID
						pendingInput.Class = prev.Click.Class
					}
				}
			} else {
				pendingInput.Text += step.Input.Text
			}
			continue
		}

		if pendingInput != nil {
			optimized = append(optimized, Step{
				Input: pendingInput,
			})
			pendingInput = nil
		}

		// Filter out duplicate consecutive clicks
		if step.Click != nil && len(optimized) > 0 {
			prevStep := optimized[len(optimized)-1]
			if prevStep.Click != nil {
				isDup := false
				// Check by target ResourceID or click coordinates
				if step.Click.Target != nil && prevStep.Click.Target != nil {
					if step.Click.Target.ResourceID != "" && step.Click.Target.ResourceID == prevStep.Click.Target.ResourceID {
						isDup = true
					}
				} else if len(step.Click.Locators) > 0 && len(prevStep.Click.Locators) > 0 {
					curr := step.Click.Locators[0]
					prev := prevStep.Click.Locators[0]
					if curr.Strategy == prev.Strategy && curr.Value == prev.Value && curr.Strategy != "coordinates" {
						isDup = true
					}
				}
				if !isDup && step.Click.X != nil && prevStep.Click.X != nil && step.Click.Y != nil && prevStep.Click.Y != nil {
					dx := *step.Click.X - *prevStep.Click.X
					dy := *step.Click.Y - *prevStep.Click.Y
					if dx*dx+dy*dy < 0.0001 {
						isDup = true
					}
				}
				if isDup {
					continue
				}
			}
		}

		optimized = append(optimized, step)
	}

	if pendingInput != nil {
		optimized = append(optimized, Step{
			Input: pendingInput,
		})
	}
	return optimized
}

// inferAssertText guesses a suitable readable name/text to assert for a package.
func inferAssertText(pkg string) string {
	common := map[string]string{
		"com.android.settings":          "Settings",
		"com.android.vending":           "Play Store",
		"com.google.android.calculator": "Calculator",
		"com.android.chrome":            "Chrome",
		"com.android.phone":             "Phone",
		"com.android.contacts":          "Contacts",
		"com.android.mms":               "Messages",
		"com.android.gallery3d":         "Gallery",
		"com.android.camera2":           "Camera",
	}
	if val, ok := common[pkg]; ok {
		return val
	}
	// Fallback: extract last component of package name and capitalize the first letter
	parts := strings.Split(pkg, ".")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if len(last) > 0 {
			return strings.ToUpper(last[:1]) + last[1:]
		}
	}
	return "Settings" // absolute fallback
}
