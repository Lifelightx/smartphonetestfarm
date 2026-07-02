package automation

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
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
		// Step retry logic
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

			// Do not retry assertions, explicit waits, or if all retries are exhausted
			if stepRes.Action == "assert" || stepRes.Action == "wait" || attempt == maxRetries {
				break
			}

			slog.Warn("automation runner: step failed, retrying...", "step", i, "attempt", attempt+1, "err", stepRes.Error)

			select {
			case <-ctx.Done():
				break
			case <-time.After(1 * time.Second): // Delay before retry
			}
		}

		report.Results = append(report.Results, stepRes)
		if stepRes.Success {
			report.PassedSteps++

			// Apply post-step delay if configured
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
			// Stop execution on first failure
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

// ------------------------------------------------
// Click Handler — Uses Ranked Locators
// ------------------------------------------------

func (r *Runner) handleClick(ctx context.Context, params *ClickParams) error {
	// New architecture: if target UIElement is specified, resolve it dynamically!
	if params.Target != nil {
		return r.handleClickWithTarget(ctx, params)
	}

	// Legacy path: use ranked locators if present
	if len(params.Locators) > 0 {
		return r.handleClickWithLocators(ctx, params)
	}

	// Legacy path: use old single-field selectors
	return r.handleClickLegacy(ctx, params)
}

func (r *Runner) handleClickWithTarget(ctx context.Context, params *ClickParams) error {
	slog.Info("automation runner: attempting click with Target UIElement", 
		"resourceId", params.Target.ResourceID, 
		"text", params.Target.Text,
		"class", params.Target.Class,
	)

	var matchedNode *UIElement
	var lastErr error

	limit := time.Now().Add(5 * time.Second)
	scrollsAttempted := 0
	maxScrolls := 3

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		xmlData, err := r.driver.DumpUI(ctx)
		if err != nil {
			lastErr = err
			time.Sleep(300 * time.Millisecond)
			continue
		}

		root, err := ParseXMLTree(xmlData)
		if err != nil {
			lastErr = err
			time.Sleep(300 * time.Millisecond)
			continue
		}

		node, _, err := ResolveElement(root, params.Target, params.Anchor)
		if err == nil {
			matchedNode = node
			break
		}
		lastErr = err

		// Attempt auto-scroll if element not resolved
		if scrollsAttempted < maxScrolls {
			scrollsAttempted++
			newXML, scrollErr := r.autoScrollToFind(ctx, scrollsAttempted)
			if scrollErr == nil && newXML != "" {
				if newRoot, newParseErr := ParseXMLTree(newXML); newParseErr == nil {
					if scrollNode, _, matchErr := ResolveElement(newRoot, params.Target, params.Anchor); matchErr == nil {
						matchedNode = scrollNode
						break
					}
				}
			}
		}

		if time.Now().After(limit) {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}

	if matchedNode == nil {
		slog.Warn("automation runner: Target UIElement resolution failed, falling back to ranked locators", "err", lastErr)
		if len(params.Locators) > 0 {
			if err := r.handleClickWithLocators(ctx, params); err == nil {
				return nil
			}
		}

		// Fallback to coordinate clicking if target and locators fail
		if params.X != nil && params.Y != nil {
			slog.Warn("automation runner: Target UIElement and locators failed, falling back to coordinates", "err", lastErr, "x", *params.X, "y", *params.Y)
			return r.driver.Tap(ctx, *params.X, *params.Y)
		}
		return fmt.Errorf("click: target and locators resolution failed: %w", lastErr)
	}

	slog.Info("automation runner: target resolved successfully", "class", matchedNode.Class, "bounds", matchedNode.Bounds)
	return r.tapUIElement(ctx, matchedNode, params.X, params.Y)
}

func (r *Runner) tapUIElement(ctx context.Context, node *UIElement, targetX, targetY *float64) error {
	width, height, err := r.driver.ScreenSize(ctx)
	if err != nil {
		return fmt.Errorf("click: failed to get screen size: %w", err)
	}

	if r.shouldTapOriginalCoords(node, targetX, targetY, width, height) {
		slog.Info("automation runner: tapping original coords on large/scroll container", "x", *targetX, "y", *targetY)
		return r.driver.Tap(ctx, *targetX, *targetY)
	}

	centerX := float64(node.Bounds.Left+node.Bounds.Right) / 2.0
	centerY := float64(node.Bounds.Top+node.Bounds.Bottom) / 2.0
	normX := centerX / float64(width)
	normY := centerY / float64(height)

	return r.driver.Tap(ctx, normX, normY)
}

func (r *Runner) shouldTapOriginalCoords(node *UIElement, targetX, targetY *float64, screenWidth, screenHeight int32) bool {
	if targetX == nil || targetY == nil {
		return false
	}

	tx := int(*targetX * float64(screenWidth))
	ty := int(*targetY * float64(screenHeight))

	// Ensure touch coordinate is inside the node's bounds
	if tx < node.Bounds.Left || tx > node.Bounds.Right || ty < node.Bounds.Top || ty > node.Bounds.Bottom {
		return false
	}

	// If it's a scroll container class, definitely use original coords
	if isScrollContainerClass(node.Class) {
		return true
	}

	// If it's a large container layout
	isContainer := strings.Contains(node.Class, "Layout") || strings.Contains(node.Class, "View") || strings.Contains(node.Class, "ViewGroup")
	if isContainer {
		nodeArea := (node.Bounds.Right - node.Bounds.Left) * (node.Bounds.Bottom - node.Bounds.Top)
		screenArea := int(screenWidth * screenHeight)
		if float64(nodeArea)/float64(screenArea) > 0.20 { // >20% of screen area
			return true
		}
	}

	return false
}

// handleClickWithLocators tries locators in descending confidence order.
func (r *Runner) handleClickWithLocators(ctx context.Context, params *ClickParams) error {
	// Sort by confidence (highest first)
	locators := make([]Locator, len(params.Locators))
	copy(locators, params.Locators)
	sort.Slice(locators, func(i, j int) bool {
		return locators[i].Confidence > locators[j].Confidence
	})

	slog.Info("automation runner: attempting click with ranked locators",
		"count", len(locators),
		"top_strategy", locators[0].Strategy,
		"top_confidence", locators[0].Confidence,
		"top_value", locators[0].Value,
	)

	// Try each non-coordinate locator
	for _, loc := range locators {
		if loc.Strategy == "coordinates" {
			continue // coordinates are last resort
		}

		match, err := r.findByLocatorWithWait(ctx, loc, params.Anchor, 5000)
		if err != nil {
			slog.Debug("automation runner: locator missed", "strategy", loc.Strategy, "value", loc.Value, "err", err)
			continue
		}

		slog.Info("automation runner: locator matched",
			"strategy", loc.Strategy,
			"value", loc.Value,
			"confidence", loc.Confidence,
			"matched_class", match.Class,
			"matched_text", match.Text,
		)

		return r.tapNode(ctx, match)
	}

	// Final fallback: coordinate locator
	for _, loc := range locators {
		if loc.Strategy == "coordinates" {
			slog.Warn("automation runner: all selectors exhausted, falling back to coordinates", "x", loc.X, "y", loc.Y)
			return r.driver.Tap(ctx, loc.X, loc.Y)
		}
	}

	// Absolute last resort: use legacy X/Y fields
	if params.X != nil && params.Y != nil {
		slog.Warn("automation runner: using legacy coordinate fallback", "x", *params.X, "y", *params.Y)
		return r.driver.Tap(ctx, *params.X, *params.Y)
	}

	return fmt.Errorf("click: all locators exhausted and no coordinates available")
}

// findByLocatorWithWait polls UI dumps until a node matching the locator is found or timeout.
func (r *Runner) findByLocatorWithWait(ctx context.Context, loc Locator, anchor *AnchorContext, timeoutMs int) (*XMLNode, error) {
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	limit := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		xmlData, err := r.driver.DumpUI(ctx)
		if err == nil {
			match, findErr := FindByLocator(xmlData, loc, anchor)
			if findErr == nil && match != nil {
				return match, nil
			}
		}

		if time.Now().After(limit) {
			return nil, fmt.Errorf("timeout waiting for locator %s=%s", loc.Strategy, loc.Value)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// tapNode calculates the center of a node's bounds and taps it.
func (r *Runner) tapNode(ctx context.Context, node *XMLNode) error {
	width, height, err := r.driver.ScreenSize(ctx)
	if err != nil {
		return fmt.Errorf("click: failed to get screen size: %w", err)
	}

	left, top, right, bottom, err := parseBounds(node.Bounds)
	if err != nil {
		return fmt.Errorf("click: failed to parse node bounds %q: %w", node.Bounds, err)
	}

	centerX := float64(left+right) / 2.0
	centerY := float64(top+bottom) / 2.0
	normX := centerX / float64(width)
	normY := centerY / float64(height)

	return r.driver.Tap(ctx, normX, normY)
}

// handleClickLegacy preserves backward compatibility with old YAML scripts
// that use single resourceId/text/xpath fields instead of locators array.
func (r *Runner) handleClickLegacy(ctx context.Context, params *ClickParams) error {
	hasStrongSelector := params.ResourceID != "" || params.ContentDesc != "" || params.Text != "" || params.XPath != ""

	if hasStrongSelector {
		slog.Info("automation runner: legacy click locating", "resourceId", params.ResourceID, "text", params.Text, "xpath", params.XPath)
		query := ElementQuery{
			ResourceID:  params.ResourceID,
			ContentDesc: params.ContentDesc,
			Text:        params.Text,
			Class:       params.Class,
			XPath:       params.XPath,
		}

		match, err := r.waitForElement(ctx, query, "visible", 5000)
		if err == nil {
			return r.tapNode(ctx, match)
		}
		slog.Warn("automation runner: legacy click selector failed, falling back to coordinates", "err", err)
	}

	if params.X != nil && params.Y != nil {
		return r.driver.Tap(ctx, *params.X, *params.Y)
	}

	return fmt.Errorf("click: no strong selector matched and no coordinates defined")
}

// ------------------------------------------------
// Input Handler
// ------------------------------------------------

func (r *Runner) handleInput(ctx context.Context, params *InputParams, vars map[string]string) error {
	// 1. Resolve variable if specified
	textToType := params.Text
	if params.Variable != "" && vars != nil {
		if val, ok := vars[params.Variable]; ok {
			textToType = val
		} else {
			slog.Warn("automation runner: variable reference not found in script variables", "variable", params.Variable)
		}
	}

	// 2. Locate the target node
	var targetNode *UIElement

	if params.Target != nil {
		// Wait and resolve target
		limit := time.Now().Add(5 * time.Second)
		scrollsAttempted := 0
		maxScrolls := 3
		for {
			xmlData, err := r.driver.DumpUI(ctx)
			if err == nil {
				root, parseErr := ParseXMLTree(xmlData)
				if parseErr == nil {
					node, _, resolveErr := ResolveElement(root, params.Target, nil)
					if resolveErr == nil {
						targetNode = node
						break
					}

					// Attempt auto-scroll if element not resolved
					if scrollsAttempted < maxScrolls {
						scrollsAttempted++
						newXML, scrollErr := r.autoScrollToFind(ctx, scrollsAttempted)
						if scrollErr == nil && newXML != "" {
							if newRoot, newParseErr := ParseXMLTree(newXML); newParseErr == nil {
								if scrollNode, _, matchErr := ResolveElement(newRoot, params.Target, nil); matchErr == nil {
									targetNode = scrollNode
									break
								}
							}
						}
					}
				}
			}
			if time.Now().After(limit) {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	// Fallback to legacy path if targetNode is nil and selector is present
	if targetNode == nil {
		hasSelector := params.ResourceID != "" || params.ContentDesc != "" || params.Class != "" || params.XPath != ""
		if hasSelector {
			query := ElementQuery{
				ResourceID:  params.ResourceID,
				ContentDesc: params.ContentDesc,
				Class:       params.Class,
				XPath:       params.XPath,
			}
			legacyNode, _ := r.waitForElement(ctx, query, "visible", 5000)
			if legacyNode != nil {
				left, top, right, bottom, _ := parseBounds(legacyNode.Bounds)
				targetNode = &UIElement{
					ResourceID: legacyNode.ResourceID,
					Class:      legacyNode.Class,
					Text:       legacyNode.Text,
					Focused:    legacyNode.Focused == "true",
					Bounds:     Rect{Left: left, Top: top, Right: right, Bottom: bottom},
				}
			}
		}
	}

	// If no explicit node found, fall back to currently focused or first EditText node
	if targetNode == nil {
		xmlData, err := r.driver.DumpUI(ctx)
		if err == nil {
			focusedNode, errFocused := FindFocusedOrEditTextNode(xmlData)
			if errFocused == nil && focusedNode != nil {
				left, top, right, bottom, _ := parseBounds(focusedNode.Bounds)
				targetNode = &UIElement{
					ResourceID: focusedNode.ResourceID,
					Class:      focusedNode.Class,
					Text:       focusedNode.Text,
					Focused:    focusedNode.Focused == "true",
					Bounds:     Rect{Left: left, Top: top, Right: right, Bottom: bottom},
				}
			}
		}
	}

	// 3. Click to focus if not already focused
	if targetNode != nil {
		if !targetNode.Focused {
			_ = r.tapUIElement(ctx, targetNode, params.X, params.Y)
			time.Sleep(300 * time.Millisecond) // Wait for focus transition
		}
	}

	// 4. Inject text input
	err := r.driver.Input(ctx, textToType)
	if err != nil {
		return err
	}

	// 5. Verify text input (if target node can be identified)
	if targetNode != nil && targetNode.ResourceID != "" {
		time.Sleep(500 * time.Millisecond) // Wait for UI update
		xmlData, err := r.driver.DumpUI(ctx)
		if err == nil {
			root, parseErr := ParseXMLTree(xmlData)
			if parseErr == nil {
				updatedNode, _, resolveErr := ResolveElement(root, targetNode, nil)
				if resolveErr == nil && updatedNode != nil {
					isPassword := strings.Contains(strings.ToLower(updatedNode.Class), "password")
					if !isPassword && updatedNode.Text != textToType && !strings.Contains(updatedNode.Text, textToType) {
						slog.Warn("automation runner: input verification warning", "expected", textToType, "got", updatedNode.Text)
					}
				}
			}
		}
	}

	return nil
}

// ------------------------------------------------
// Wait / Assert / Condition Handlers
// ------------------------------------------------

func (r *Runner) handleWait(ctx context.Context, params *WaitParams) error {
	condition := params.Condition
	if condition == "" {
		condition = "visible"
	}

	timeoutMs := params.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	if params.Target != nil {
		limit := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}

			xmlData, err := r.driver.DumpUI(ctx)
			var node *UIElement
			if err == nil {
				root, parseErr := ParseXMLTree(xmlData)
				if parseErr == nil {
					node, _, _ = ResolveElement(root, params.Target, nil)
				}
			}

			if condition == "visible" || condition == "present" {
				if node != nil {
					return nil
				}
			} else if condition == "hidden" {
				if node == nil {
					return nil
				}
			}

			if time.Now().After(limit) {
				return fmt.Errorf("timeout waiting for target UIElement to match condition %s", condition)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
	}

	query := ElementQuery{
		ResourceID:  params.ResourceID,
		ContentDesc: params.ContentDesc,
		Text:        params.Text,
		Class:       params.Class,
		XPath:       params.XPath,
	}

	_, err := r.waitForElement(ctx, query, condition, timeoutMs)
	return err
}

func (r *Runner) handleAssert(ctx context.Context, params *AssertParams) error {
	timeoutMs := params.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}

	condition := params.Condition
	if condition == "" {
		condition = "visible"
	}

	if params.Target != nil {
		limit := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
		var matchedNode *UIElement
		var lastErr error

		for {
			if err := ctx.Err(); err != nil {
				return err
			}

			xmlData, err := r.driver.DumpUI(ctx)
			if err == nil {
				root, parseErr := ParseXMLTree(xmlData)
				if parseErr == nil {
					node, _, resolveErr := ResolveElement(root, params.Target, nil)
					if resolveErr == nil {
						matchedNode = node
					} else {
						lastErr = resolveErr
					}
				}
			}

			if condition == "hidden" {
				if matchedNode == nil {
					return nil
				}
			} else {
				if matchedNode != nil {
					break
				}
			}

			if time.Now().After(limit) {
				if condition == "hidden" {
					return fmt.Errorf("assert failed: element is still visible")
				}
				if lastErr != nil {
					return fmt.Errorf("assert failed: element is not visible: %w", lastErr)
				}
				return fmt.Errorf("assert failed: element is not visible")
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}

		if condition == "visible" || condition == "present" {
			return nil
		}

		val := params.Value
		if val == "" && params.Target != nil {
			val = params.Target.Text
		}

		switch condition {
		case "equals":
			if matchedNode.Text != val {
				return fmt.Errorf("assert failed: expected text %q, got %q", val, matchedNode.Text)
			}
			return nil
		case "contains":
			if !strings.Contains(matchedNode.Text, val) {
				return fmt.Errorf("assert failed: text %q does not contain %q", matchedNode.Text, val)
			}
			return nil
		default:
			return fmt.Errorf("assert failed: unknown assertion condition %q", condition)
		}
	}

	query := ElementQuery{
		ResourceID:  params.ResourceID,
		ContentDesc: params.ContentDesc,
		Text:        params.Text,
		Class:       params.Class,
		XPath:       params.XPath,
	}

	if condition == "hidden" {
		_, err := r.waitForElement(ctx, query, "hidden", timeoutMs)
		if err != nil {
			return fmt.Errorf("assert failed: element is still visible: %w", err)
		}
		return nil
	}

	match, err := r.waitForElement(ctx, query, "visible", timeoutMs)
	if err != nil {
		return fmt.Errorf("assert failed: element is not visible: %w", err)
	}

	val := params.Value
	if val == "" {
		val = params.Text
	}

	switch condition {
	case "visible", "present":
		return nil

	case "equals":
		if match.Text != val {
			return fmt.Errorf("assert failed: expected text %q, got %q", val, match.Text)
		}
		return nil

	case "contains":
		if !strings.Contains(match.Text, val) {
			return fmt.Errorf("assert failed: text %q does not contain %q", match.Text, val)
		}
		return nil

	default:
		return fmt.Errorf("assert failed: unknown assertion condition %q", condition)
	}
}

// waitForElement polls UI dumps until the target element query matches the condition.
func (r *Runner) waitForElement(ctx context.Context, query ElementQuery, condition string, timeoutMs int) (*XMLNode, error) {
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	limit := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		xmlData, err := r.driver.DumpUI(ctx)
		if err == nil {
			var hierarchy UIHierarchy
			if errXml := xml.Unmarshal([]byte(xmlData), &hierarchy); errXml == nil {
				var match *XMLNode

				// Try XPath first if specified
				if query.XPath != "" {
					match = EvaluateXPath(&hierarchy, query.XPath)
				}

				// Fall back to attribute matching
				if match == nil {
					for i := range hierarchy.Nodes {
						if found := searchNode(&hierarchy.Nodes[i], query); found != nil {
							match = found
							break
						}
					}
				}

				if condition == "visible" || condition == "present" {
					if match != nil {
						return match, nil
					}
				} else if condition == "hidden" {
					if match == nil {
						return nil, nil
					}
				}
			}
		}

		if time.Now().After(limit) {
			if condition == "hidden" {
				return nil, fmt.Errorf("timeout waiting for element to be hidden")
			}
			return nil, fmt.Errorf("timeout waiting for element to match query %+v", query)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// evaluateCondition determines if the given conditional rule is satisfied.
func (r *Runner) evaluateCondition(ctx context.Context, cond *IfCondition) bool {
	if cond == nil || cond.Exists == nil {
		return false
	}

	if cond.Exists.Target != nil {
		xmlData, err := r.driver.DumpUI(ctx)
		if err == nil {
			root, parseErr := ParseXMLTree(xmlData)
			if parseErr == nil {
				_, _, resolveErr := ResolveElement(root, cond.Exists.Target, nil)
				return resolveErr == nil
			}
		}
		return false
	}

	query := ElementQuery{
		ResourceID:  cond.Exists.ResourceID,
		ContentDesc: cond.Exists.ContentDesc,
		Text:        cond.Exists.Text,
		Class:       cond.Exists.Class,
		XPath:       cond.Exists.XPath,
	}

	_, err := r.waitForElement(ctx, query, "visible", 1000)
	return err == nil
}

// waitForUIStabilization polls the UI hierarchy dumps until the XML content remains stable
// (i.e. identical structure/content) for two consecutive checks or timeout is reached.
func (r *Runner) waitForUIStabilization(ctx context.Context, timeout time.Duration) error {
	limit := time.Now().Add(timeout)
	lastXML := ""
	pollInterval := 150 * time.Millisecond

	for time.Now().Before(limit) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		xmlData, err := r.driver.DumpUI(ctx)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		if lastXML != "" && xmlData == lastXML {
			slog.Debug("automation runner: UI hierarchy is stable")
			return nil
		}

		lastXML = xmlData
		time.Sleep(pollInterval)
	}

	slog.Warn("automation runner: UI stabilization timeout reached; proceeding anyway")
	return nil
}

// autoScrollToFind targets a scrollable container in the current UI XML and performs a swipe
// to expose off-screen elements, returning the fresh XML hierarchy if a scroll occurred.
func (r *Runner) autoScrollToFind(ctx context.Context, scrollCount int) (string, error) {
	xmlData, err := r.driver.DumpUI(ctx)
	if err != nil {
		return "", err
	}

	root, err := ParseXMLTree(xmlData)
	if err != nil {
		return "", err
	}

	var scrollNode *UIElement
	elements := root.FlattenTree()
	for _, el := range elements {
		if el.Scrollable || isScrollContainerClass(el.Class) {
			scrollNode = el
			break
		}
	}

	if scrollNode == nil {
		return "", fmt.Errorf("no scrollable container found on screen")
	}

	bounds := scrollNode.Bounds
	width, height, err := r.driver.ScreenSize(ctx)
	if err != nil {
		return "", err
	}

	// Calculate swipe coordinates within the container bounds
	centerX := float64(bounds.Left+bounds.Right) / 2.0 / float64(width)
	startY := float64(bounds.Bottom) * 0.8 / float64(height)
	endY := float64(bounds.Top) * 0.2 / float64(height)

	if startY > 0.9 {
		startY = 0.8
	}
	if endY < 0.1 {
		endY = 0.2
	}

	slog.Info("automation runner: auto-scrolling to find element", "scrollCount", scrollCount, "container", scrollNode.Class)
	err = r.driver.Swipe(ctx, centerX, startY, centerX, endY, 800)
	if err != nil {
		return "", fmt.Errorf("auto-scroll swipe failed: %w", err)
	}

	time.Sleep(600 * time.Millisecond)
	_ = r.waitForUIStabilization(ctx, 1500*time.Millisecond)

	return r.driver.DumpUI(ctx)
}

func (r *Runner) verifyPackageActive(ctx context.Context, expectedPackage string, timeoutMs int) error {
	limit := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(limit) {
		xmlData, err := r.driver.DumpUI(ctx)
		if err == nil {
			root, parseErr := ParseXMLTree(xmlData)
			if parseErr == nil && root.Package == expectedPackage {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	slog.Warn("automation runner: package verification warning - active package does not match launched package", "expected", expectedPackage)
	return nil
}
