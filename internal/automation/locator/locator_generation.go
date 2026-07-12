// Package locator implements test script execution, scheduling, compiling, and locators (locator).
//
// File: locator_generation.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators (locator).

package locator

import (
	"encoding/xml"
	"fmt"
	"sort"

	"protean-provider/internal/automation/dsl"
)

// ExtractAnchor extracts sibling text and parent context for disambiguation.
func ExtractAnchor(xmlData string, targetNode *dsl.XMLNode) *dsl.AnchorContext {
	if targetNode == nil {
		return nil
	}

	var hierarchy dsl.UIHierarchy
	if err := xml.Unmarshal([]byte(xmlData), &hierarchy); err != nil {
		return nil
	}

	anchor := &dsl.AnchorContext{}
	findParentAndSiblings(&hierarchy, targetNode, anchor)

	if anchor.SiblingText == "" && anchor.ParentID == "" {
		return nil
	}
	return anchor
}

// findParentAndSiblings performs the find parent and siblings operation.
func findParentAndSiblings(h *dsl.UIHierarchy, target *dsl.XMLNode, anchor *dsl.AnchorContext) bool {
	for i := range h.Nodes {
		if findParentAndSiblingsInNode(&h.Nodes[i], target, anchor) {
			return true
		}
	}
	return false
}

// findParentAndSiblingsInNode performs the find parent and siblings in node operation.
func findParentAndSiblingsInNode(parent *dsl.XMLNode, target *dsl.XMLNode, anchor *dsl.AnchorContext) bool {
	for i := range parent.Nodes {
		child := &parent.Nodes[i]
		if child.Bounds == target.Bounds && child.Class == target.Class && child.ResourceID == target.ResourceID && child.Text == target.Text {
			anchor.ParentID = parent.ResourceID
			anchor.ParentClass = parent.Class

			for j := range parent.Nodes {
				sibling := &parent.Nodes[j]
				if j == i {
					continue
				}
				if sibling.Text != "" {
					anchor.SiblingText = sibling.Text
					break
				}
				if textNode := findFirstTextNode(sibling); textNode != nil {
					anchor.SiblingText = textNode.Text
					break
				}
			}
			return true
		}
		if findParentAndSiblingsInNode(child, target, anchor) {
			return true
		}
	}
	return false
}

// findFirstTextNode performs the find first text node operation.
func findFirstTextNode(node *dsl.XMLNode) *dsl.XMLNode {
	if node.Text != "" {
		return node
	}
	for i := range node.Nodes {
		if found := findFirstTextNode(&node.Nodes[i]); found != nil {
			return found
		}
	}
	return nil
}

// GenerateLocators produces a ranked list of locator strategies for a target node.
func GenerateLocators(xmlData string, targetNode *dsl.XMLNode, x, y float64) []dsl.Locator {
	if targetNode == nil {
		return []dsl.Locator{{Strategy: "coordinates", Value: fmt.Sprintf("%.6f,%.6f", x, y), Confidence: 10, X: x, Y: y}}
	}

	var hierarchy dsl.UIHierarchy
	if err := xml.Unmarshal([]byte(xmlData), &hierarchy); err != nil {
		return []dsl.Locator{{Strategy: "coordinates", Value: fmt.Sprintf("%.6f,%.6f", x, y), Confidence: 10, X: x, Y: y}}
	}

	var locators []dsl.Locator

	if targetNode.ResourceID != "" {
		isContainer := IsContainerClass(targetNode.Class)
		hasIdentity := targetNode.Text != "" || targetNode.ContentDesc != ""
		count := countMatching(&hierarchy, func(n *dsl.XMLNode) bool {
			return n.ResourceID == targetNode.ResourceID
		})
		switch {
		case isContainer && !hasIdentity:
			locators = append(locators, dsl.Locator{Strategy: "resourceId", Value: targetNode.ResourceID, Confidence: 20})
		case count == 1:
			locators = append(locators, dsl.Locator{Strategy: "resourceId", Value: targetNode.ResourceID, Confidence: 100})
		default:
			locators = append(locators, dsl.Locator{Strategy: "resourceId", Value: targetNode.ResourceID, Confidence: 40})
		}
	}

	if targetNode.Text != "" {
		count := countMatching(&hierarchy, func(n *dsl.XMLNode) bool {
			return n.Text == targetNode.Text
		})
		if count == 1 {
			locators = append(locators, dsl.Locator{Strategy: "text", Value: targetNode.Text, Confidence: 95})
		} else {
			locators = append(locators, dsl.Locator{Strategy: "text", Value: targetNode.Text, Confidence: 60})
		}
	}

	if targetNode.ContentDesc != "" {
		count := countMatching(&hierarchy, func(n *dsl.XMLNode) bool {
			return n.ContentDesc == targetNode.ContentDesc
		})
		if count == 1 {
			locators = append(locators, dsl.Locator{Strategy: "contentDesc", Value: targetNode.ContentDesc, Confidence: 90})
		} else {
			locators = append(locators, dsl.Locator{Strategy: "contentDesc", Value: targetNode.ContentDesc, Confidence: 55})
		}
	}

	if targetNode.Class != "" {
		locators = append(locators, dsl.Locator{
			Strategy:   "class",
			Value:      targetNode.Class,
			Confidence: 30,
		})
	}

	// Generate XPath locator
	var findXPath func(n *dsl.XMLNode, target *dsl.XMLNode, path string) (string, bool)
	findXPath = func(n *dsl.XMLNode, target *dsl.XMLNode, path string) (string, bool) {
		if n == target {
			return path, true
		}
		for i := range n.Nodes {
			childPath := fmt.Sprintf("%s/node[@index='%s']", path, n.Nodes[i].Index)
			if xp, found := findXPath(&n.Nodes[i], target, childPath); found {
				return xp, true
			}
		}
		return "", false
	}

	var xpathVal string
	for i := range hierarchy.Nodes {
		rootPath := fmt.Sprintf("/hierarchy/node[@index='%s']", hierarchy.Nodes[i].Index)
		if xp, found := findXPath(&hierarchy.Nodes[i], targetNode, rootPath); found {
			xpathVal = xp
			break
		}
	}

	if xpathVal != "" {
		locators = append(locators, dsl.Locator{
			Strategy:   "xpath",
			Value:      xpathVal,
			Confidence: 20,
		})
	}

	locators = append(locators, dsl.Locator{Strategy: "coordinates", Value: fmt.Sprintf("%.6f,%.6f", x, y), Confidence: 10, X: x, Y: y})

	sort.Slice(locators, func(i, j int) bool {
		return locators[i].Confidence > locators[j].Confidence
	})

	return locators
}

// countMatching performs the count matching operation.
func countMatching(h *dsl.UIHierarchy, pred func(*dsl.XMLNode) bool) int {
	count := 0
	var walk func(n *dsl.XMLNode)
	walk = func(n *dsl.XMLNode) {
		if pred(n) {
			count++
		}
		for i := range n.Nodes {
			walk(&n.Nodes[i])
		}
	}
	for i := range h.Nodes {
		walk(&h.Nodes[i])
	}
	return count
}
