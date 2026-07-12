// Package locator implements test script execution, scheduling, compiling, and locators (locator).
//
// File: locator_scoring.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators (locator).

package locator

import (
	"fmt"
	"sort"
	"strings"

	"protean-provider/internal/automation/dsl"
)

// IsScrollContainerClass returns true for container classes that scroll or hold lists.
func IsScrollContainerClass(cls string) bool {
	scrollContainers := []string{
		"androidx.recyclerview.widget.RecyclerView",
		"android.widget.ListView",
		"android.widget.GridView",
		"android.widget.ScrollView",
		"android.widget.HorizontalScrollView",
		"androidx.viewpager.widget.ViewPager",
		"androidx.viewpager2.widget.ViewPager2",
	}
	for _, c := range scrollContainers {
		if cls == c || strings.HasSuffix(cls, c) {
			return true
		}
	}
	return false
}

// IsContainerClass returns true for layout/container classes that are almost never click targets.
func IsContainerClass(cls string) bool {
	containers := []string{
		"android.widget.FrameLayout",
		"android.widget.LinearLayout",
		"android.widget.RelativeLayout",
		"android.view.ViewGroup",
		"android.view.View",
		"androidx.recyclerview.widget.RecyclerView",
		"android.widget.ScrollView",
		"android.widget.HorizontalScrollView",
		"android.widget.ListView",
		"androidx.viewpager.widget.ViewPager",
		"androidx.viewpager2.widget.ViewPager2",
		"androidx.constraintlayout.widget.ConstraintLayout",
		"androidx.coordinatorlayout.widget.CoordinatorLayout",
		"androidx.appcompat.widget.LinearLayoutCompat",
		"android.widget.GridLayout",
		"android.widget.TableLayout",
		"android.widget.TableRow",
	}
	for _, c := range containers {
		if cls == c || strings.HasSuffix(cls, c) {
			return true
		}
	}
	return false
}

// ScoreActionableNode scores a UIElement based on its appropriateness as a click target.
func ScoreActionableNode(node *dsl.UIElement, screenWidth, screenHeight int32) int {
	score := 0

	// Interactivity signals
	if node.Clickable {
		score += 100
	}
	if node.Enabled {
		score += 50
	}
	if node.ResourceID != "" {
		score += 20
	}
	if node.Text != "" {
		score += 15
	}
	if node.ContentDesc != "" {
		score += 15
	}

	// Interactive widget class bonuses
	cls := node.Class
	switch {
	case cls == "android.widget.Button":
		score += 30
	case cls == "android.widget.ImageButton":
		score += 30
	case cls == "android.widget.Switch" || cls == "android.widget.ToggleButton":
		score += 30
	case cls == "android.widget.CheckBox":
		score += 25
	case cls == "android.widget.RadioButton":
		score += 25
	case cls == "android.widget.EditText":
		score += 25
	case cls == "android.widget.TextView":
		score += 10
	case cls == "android.widget.ImageView":
		score += 5
	}

	// Container penalties
	if IsScrollContainerClass(cls) {
		score -= 200 // Scroll containers are almost never the direct click target
	} else if IsContainerClass(cls) {
		score -= 80 // Standard layouts
	} else if cls == "android.view.View" {
		score -= 120 // Empty/raw views are usually transparent overlays
	}

	// Area-based penalty: larger elements are less likely to be the specific target
	area := (node.Bounds.Right - node.Bounds.Left) * (node.Bounds.Bottom - node.Bounds.Top)
	if screenWidth > 0 && screenHeight > 0 && area > 0 {
		screenArea := int(screenWidth * screenHeight)
		ratio := float64(area) / float64(screenArea)
		if ratio > 0.5 {
			score -= 150
		} else if ratio > 0.2 {
			score -= 80
		} else if ratio > 0.05 {
			score -= 30
		}
	}

	return score
}

// ScoreNode scores a UI node for click-target likelihood (kept for compatibility).
func ScoreNode(node *dsl.XMLNode, screenWidth, screenHeight int32) int {
	// Map XMLNode to UIElement to reuse the core scoring logic
	left, top, right, bottom, _ := ParseBounds(node.Bounds)
	el := &dsl.UIElement{
		Class:       node.Class,
		Clickable:   node.Clickable == "true",
		Enabled:     node.Enabled == "true",
		ResourceID:  node.ResourceID,
		Text:        node.Text,
		ContentDesc: node.ContentDesc,
		Bounds: dsl.Rect{
			Left:   left,
			Top:    top,
			Right:  right,
			Bottom: bottom,
		},
	}
	return ScoreActionableNode(el, screenWidth, screenHeight)
}

// FindBestElementAt coordinates hit-testing logic for a given point.
func FindBestElementAt(root *dsl.UIElement, x, y float64, width, height int32) *dsl.UIElement {
	px := int(x * float64(width))
	py := int(y * float64(height))

	var candidates []*dsl.UIElement

	// Helper to find all elements containing (px, py)
	var walk func(n *dsl.UIElement)
	walk = func(n *dsl.UIElement) {
		if px < n.Bounds.Left || px > n.Bounds.Right || py < n.Bounds.Top || py > n.Bounds.Bottom {
			return
		}
		candidates = append(candidates, n)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)

	if len(candidates) == 0 {
		return nil
	}

	// For each candidate, walk upwards to find the first clickable/focusable/longclickable ancestor
	var actionableCandidates []*dsl.UIElement
	seen := make(map[*dsl.UIElement]bool)

	for _, cand := range candidates {
		curr := cand
		for curr != nil {
			isListItem := curr.Parent != nil && IsScrollContainerClass(curr.Parent.Class)
			if curr.Clickable || curr.Focusable || curr.LongClickable || isListItem {
				if !seen[curr] {
					seen[curr] = true
					actionableCandidates = append(actionableCandidates, curr)
				}
				break
			}
			curr = curr.Parent
		}
	}

	// Fallback: if no actionable candidates are found, score all candidates containing the touch point
	if len(actionableCandidates) == 0 {
		actionableCandidates = candidates
	}

	// Sort actionable candidates by score (highest first), tie-break by smaller area
	sort.Slice(actionableCandidates, func(i, j int) bool {
		scoreI := ScoreActionableNode(actionableCandidates[i], width, height)
		scoreJ := ScoreActionableNode(actionableCandidates[j], width, height)
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		areaI := (actionableCandidates[i].Bounds.Right - actionableCandidates[i].Bounds.Left) * (actionableCandidates[i].Bounds.Bottom - actionableCandidates[i].Bounds.Top)
		areaJ := (actionableCandidates[j].Bounds.Right - actionableCandidates[j].Bounds.Left) * (actionableCandidates[j].Bounds.Bottom - actionableCandidates[j].Bounds.Top)
		return areaI < areaJ
	})

	return actionableCandidates[0]
}

// FindBestNodeAt parses the XML tree, uses FindBestElementAt, and returns the corresponding XMLNode and XPath.
func FindBestNodeAt(xmlData string, x, y float64, width, height int32) (*dsl.XMLNode, string) {
	root, err := ParseXMLTree(xmlData)
	if err != nil {
		return nil, ""
	}

	bestEl := FindBestElementAt(root, x, y, width, height)
	if bestEl == nil || bestEl.XMLRef == nil {
		return nil, ""
	}

	// Build XPath for the XMLRef node
	xpath := buildXPathForElement(bestEl)
	return bestEl.XMLRef, xpath
}

// buildXPathForElement performs the build xpath for element operation.
func buildXPathForElement(el *dsl.UIElement) string {
	var parts []string
	curr := el
	for curr != nil {
		if curr.XMLRef != nil {
			parts = append([]string{fmt.Sprintf("node[@index='%s']", curr.XMLRef.Index)}, parts...)
		} else {
			parts = append([]string{"node"}, parts...)
		}
		curr = curr.Parent
	}
	return "/hierarchy/" + strings.Join(parts, "/")
}
