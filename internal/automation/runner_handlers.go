// Package automation implements test script execution, scheduling, compiling, and locators.
//
// File: runner_handlers.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators.

package automation

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// ------------------------------------------------
// Click Handler — Uses Ranked Locators
// ------------------------------------------------

func (r *Runner) handleClick(ctx context.Context, params *ClickParams) error {
	if params.Target != nil {
		return r.handleClickWithTarget(ctx, params)
	}

	if len(params.Locators) > 0 {
		return r.handleClickWithLocators(ctx, params)
	}

	return r.handleClickLegacy(ctx, params)
}

// handleClickWithTarget handles the click with target request/event.
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

		if params.X != nil && params.Y != nil {
			slog.Warn("automation runner: Target UIElement and locators failed, falling back to coordinates", "err", lastErr, "x", *params.X, "y", *params.Y)
			return r.driver.Tap(ctx, *params.X, *params.Y)
		}
		return fmt.Errorf("click: target and locators resolution failed: %w", lastErr)
	}

	slog.Info("automation runner: target resolved successfully", "class", matchedNode.Class, "bounds", matchedNode.Bounds)
	return r.tapUIElement(ctx, matchedNode, params.X, params.Y)
}

// handleClickWithLocators handles the click with locators request/event.
func (r *Runner) handleClickWithLocators(ctx context.Context, params *ClickParams) error {
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

	for _, loc := range locators {
		if loc.Strategy == "coordinates" {
			continue
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

	for _, loc := range locators {
		if loc.Strategy == "coordinates" {
			slog.Warn("automation runner: all selectors exhausted, falling back to coordinates", "x", loc.X, "y", loc.Y)
			return r.driver.Tap(ctx, loc.X, loc.Y)
		}
	}

	if params.X != nil && params.Y != nil {
		slog.Warn("automation runner: using legacy coordinate fallback", "x", *params.X, "y", *params.Y)
		return r.driver.Tap(ctx, *params.X, *params.Y)
	}

	return fmt.Errorf("click: all locators exhausted and no coordinates available")
}

// handleClickLegacy handles the click legacy request/event.
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
	textToType := params.Text
	if params.Variable != "" && vars != nil {
		if val, ok := vars[params.Variable]; ok {
			textToType = val
		} else {
			slog.Warn("automation runner: variable reference not found in script variables", "variable", params.Variable)
		}
	}

	var targetNode *UIElement

	if params.Target != nil {
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

	if targetNode != nil {
		if !targetNode.Focused {
			_ = r.tapUIElement(ctx, targetNode, params.X, params.Y)
			time.Sleep(300 * time.Millisecond)
		}
	}

	err := r.driver.Input(ctx, textToType)
	if err != nil {
		return err
	}

	if targetNode != nil && targetNode.ResourceID != "" {
		time.Sleep(500 * time.Millisecond)
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

// handleAssert handles the assert request/event.
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
