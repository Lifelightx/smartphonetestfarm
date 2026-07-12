// Package locator implements test script execution, scheduling, compiling, and locators (locator).
//
// File: locator_tree.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators (locator).

package locator

import (
	"encoding/xml"
	"fmt"

	"protean-provider/internal/automation/dsl"
)

// ParseXMLTree parses a UiAutomator XML dump into a tree of UIElement objects.
func ParseXMLTree(xmlData string) (*dsl.UIElement, error) {
	var hierarchy dsl.UIHierarchy
	if err := xml.Unmarshal([]byte(xmlData), &hierarchy); err != nil {
		return nil, fmt.Errorf("failed to parse UI XML: %w", err)
	}

	if len(hierarchy.Nodes) == 0 {
		return nil, fmt.Errorf("empty UI hierarchy")
	}

	if len(hierarchy.Nodes) == 1 {
		return convertXMLToUIElement(&hierarchy.Nodes[0], nil), nil
	}

	// Wrap multiple top-level nodes in a virtual root
	virtualRoot := &dsl.UIElement{
		Class: "hierarchy",
	}
	for i := range hierarchy.Nodes {
		virtualRoot.Children = append(virtualRoot.Children, convertXMLToUIElement(&hierarchy.Nodes[i], virtualRoot))
	}
	// Deduce virtual bounds as the union of child bounds
	left, top, right, bottom := 99999, 99999, 0, 0
	for _, child := range virtualRoot.Children {
		if child.Bounds.Left < left {
			left = child.Bounds.Left
		}
		if child.Bounds.Top < top {
			top = child.Bounds.Top
		}
		if child.Bounds.Right > right {
			right = child.Bounds.Right
		}
		if child.Bounds.Bottom > bottom {
			bottom = child.Bounds.Bottom
		}
	}
	virtualRoot.Bounds = dsl.Rect{Left: left, Top: top, Right: right, Bottom: bottom}
	return virtualRoot, nil
}

// convertXMLToUIElement performs the convert xmlto uielement operation.
func convertXMLToUIElement(node *dsl.XMLNode, parent *dsl.UIElement) *dsl.UIElement {
	left, top, right, bottom, _ := ParseBounds(node.Bounds)
	el := &dsl.UIElement{
		Package:     node.Package,
		ResourceID:  node.ResourceID,
		Text:        node.Text,
		ContentDesc: node.ContentDesc,
		Class:       node.Class,
		Bounds: dsl.Rect{
			Left:   left,
			Top:    top,
			Right:  right,
			Bottom: bottom,
		},
		Clickable:     node.Clickable == "true",
		Enabled:       node.Enabled == "true",
		Focusable:     node.Focusable == "true",
		Focused:       node.Focused == "true",
		Checked:       node.Checked == "true",
		Selected:      node.Selected == "true",
		LongClickable: node.LongClickable == "true",
		Checkable:     node.Checkable == "true",
		Scrollable:    node.Scrollable == "true",
		Parent:        parent,
		XMLRef:        node,
	}
	for i := range node.Nodes {
		el.Children = append(el.Children, convertXMLToUIElement(&node.Nodes[i], el))
	}
	return el
}

// ParseBounds performs the parse bounds operation.
func ParseBounds(boundsStr string) (left, top, right, bottom int, err error) {
	_, err = fmt.Sscanf(boundsStr, "[%d,%d][%d,%d]", &left, &top, &right, &bottom)
	return
}
