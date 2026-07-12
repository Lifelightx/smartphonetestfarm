// Package automation implements test script execution, scheduling, compiling, and locators.
//
// File: runner_helpers.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators.

package automation

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// tapUIElement performs the tap uielement operation.
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

// shouldTapOriginalCoords performs the should tap original coords operation.
func (r *Runner) shouldTapOriginalCoords(node *UIElement, targetX, targetY *float64, screenWidth, screenHeight int32) bool {
	if targetX == nil || targetY == nil {
		return false
	}

	tx := int(*targetX * float64(screenWidth))
	ty := int(*targetY * float64(screenHeight))

	if tx < node.Bounds.Left || tx > node.Bounds.Right || ty < node.Bounds.Top || ty > node.Bounds.Bottom {
		return false
	}

	if isScrollContainerClass(node.Class) {
		return true
	}

	isContainer := strings.Contains(node.Class, "Layout") || strings.Contains(node.Class, "View") || strings.Contains(node.Class, "ViewGroup")
	if isContainer {
		nodeArea := (node.Bounds.Right - node.Bounds.Left) * (node.Bounds.Bottom - node.Bounds.Top)
		screenArea := int(screenWidth * screenHeight)
		if float64(nodeArea)/float64(screenArea) > 0.20 {
			return true
		}
	}

	return false
}

// findByLocatorWithWait performs the find by locator with wait operation.
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

// tapNode performs the tap node operation.
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

// waitForElement performs the wait for element operation.
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

				if query.XPath != "" {
					match = EvaluateXPath(&hierarchy, query.XPath)
				}

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

// waitForUIStabilization performs the wait for uistabilization operation.
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

// autoScrollToFind performs the auto scroll to find operation.
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

// verifyPackageActive performs the verify package active operation.
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
