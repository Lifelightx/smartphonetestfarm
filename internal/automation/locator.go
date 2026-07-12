// Package automation implements test script execution, scheduling, compiling, and locators.
//
// File: locator.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators.

package automation

import (
	"protean-provider/internal/automation/locator"
)

// Facade / Function forwards to the locator subpackage

var ParseXMLTree = locator.ParseXMLTree
var ResolveElement = locator.ResolveElement
var FindBestElementAt = locator.FindBestElementAt
var FindBestNodeAt = locator.FindBestNodeAt
var ExtractAnchor = locator.ExtractAnchor
var GenerateLocators = locator.GenerateLocators
var FindByLocator = locator.FindByLocator
var FindElement = locator.FindElement
var FindFocusedOrEditTextNode = locator.FindFocusedOrEditTextNode
var EvaluateXPath = locator.EvaluateXPath
var ScoreActionableNode = locator.ScoreActionableNode
var ScoreNode = locator.ScoreNode

// Package-internal forwards to unburden call sites in runner
var parseBounds = locator.ParseBounds
var isScrollContainerClass = locator.IsScrollContainerClass
var searchNode = locator.SearchNode
