// Package locator implements test script execution, scheduling, compiling, and locators (locator).
//
// File: locator_legacy.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators (locator).

package locator

import (
	"encoding/xml"
	"fmt"
	"strings"

	"protean-provider/internal/automation/dsl"
)

// Legacy Support

func FindElement(xmlData string, query dsl.ElementQuery, width, height int32) (float64, float64, error) {
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid screen dimensions: %dx%d", width, height)
	}

	root, err := ParseXMLTree(xmlData)
	if err != nil {
		return 0, 0, err
	}

	target := &dsl.UIElement{
		ResourceID:  query.ResourceID,
		ContentDesc: query.ContentDesc,
		Text:        query.Text,
		Class:       query.Class,
	}

	bestMatch, _, err := ResolveElement(root, target, nil)
	if err != nil {
		return 0, 0, err
	}

	centerX := float64(bestMatch.Bounds.Left+bestMatch.Bounds.Right) / 2.0
	centerY := float64(bestMatch.Bounds.Top+bestMatch.Bounds.Bottom) / 2.0

	return centerX / float64(width), centerY / float64(height), nil
}

// SearchNode performs the search node operation.
func SearchNode(node *dsl.XMLNode, query dsl.ElementQuery) *dsl.XMLNode {
	if matchesQuery(node, query) {
		return node
	}
	for i := range node.Nodes {
		if found := SearchNode(&node.Nodes[i], query); found != nil {
			return found
		}
	}
	return nil
}

// matchesQuery performs the matches query operation.
func matchesQuery(node *dsl.XMLNode, query dsl.ElementQuery) bool {
	if query.ResourceID != "" {
		return node.ResourceID == query.ResourceID || strings.HasSuffix(node.ResourceID, ":id/"+query.ResourceID)
	}
	if query.ContentDesc != "" {
		return node.ContentDesc == query.ContentDesc
	}
	if query.Text != "" {
		return node.Text == query.Text || strings.Contains(node.Text, query.Text)
	}
	if query.Class != "" {
		return node.Class == query.Class
	}
	return false
}

// FindFocusedOrEditTextNode performs the find focused or edit text node operation.
func FindFocusedOrEditTextNode(xmlData string) (*dsl.XMLNode, error) {
	var hierarchy dsl.UIHierarchy
	if err := xml.Unmarshal([]byte(xmlData), &hierarchy); err != nil {
		return nil, err
	}
	for i := range hierarchy.Nodes {
		if found := findNodeByFocus(&hierarchy.Nodes[i]); found != nil {
			return found, nil
		}
	}
	for i := range hierarchy.Nodes {
		if found := findNodeByClass(&hierarchy.Nodes[i], "android.widget.EditText"); found != nil {
			return found, nil
		}
	}
	return nil, fmt.Errorf("no focused or EditText node found in UI hierarchy")
}

// findNodeByFocus performs the find node by focus operation.
func findNodeByFocus(node *dsl.XMLNode) *dsl.XMLNode {
	if node.Focused == "true" {
		return node
	}
	for i := range node.Nodes {
		if found := findNodeByFocus(&node.Nodes[i]); found != nil {
			return found
		}
	}
	return nil
}

// findNodeByClass performs the find node by class operation.
func findNodeByClass(node *dsl.XMLNode, className string) *dsl.XMLNode {
	if node.Class == className {
		return node
	}
	for i := range node.Nodes {
		if found := findNodeByClass(&node.Nodes[i], className); found != nil {
			return found
		}
	}
	return nil
}
