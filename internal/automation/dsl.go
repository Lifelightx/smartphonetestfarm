// Package automation implements test script execution, scheduling, compiling, and locators.
//
// File: dsl.go
// This file contains implementation and helper structures for test script execution, scheduling, compiling, and locators.

package automation

import (
	"protean-provider/internal/automation/dsl"
)

// Facade / Type Aliases for backward compatibility and clean packages

type Rect = dsl.Rect
type UIElement = dsl.UIElement
type Script = dsl.Script
type Step = dsl.Step
type Locator = dsl.Locator
type AnchorContext = dsl.AnchorContext
type ClickParams = dsl.ClickParams
type InputParams = dsl.InputParams
type SwipeParams = dsl.SwipeParams
type WaitParams = dsl.WaitParams
type AssertParams = dsl.AssertParams
type ExistsCondition = dsl.ExistsCondition
type IfCondition = dsl.IfCondition
type LaunchParams = dsl.LaunchParams
type TerminateParams = dsl.TerminateParams
type XMLNode = dsl.XMLNode
type UIHierarchy = dsl.UIHierarchy
type ElementQuery = dsl.ElementQuery

var ParseScript = dsl.ParseScript
var ParseScriptFile = dsl.ParseScriptFile
