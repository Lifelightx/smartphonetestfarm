package dsl

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// UIHierarchy represents the root element of a UiAutomator XML dump.
type UIHierarchy struct {
	XMLName  xml.Name  `xml:"hierarchy"`
	Rotation string    `xml:"rotation,attr"`
	Nodes    []XMLNode `xml:"node"`
}

// XMLNode represents a single element node in the UiAutomator UI hierarchy XML dump.
type XMLNode struct {
	XMLName       xml.Name  `xml:"node"`
	Index         string    `xml:"index,attr"`
	Text          string    `xml:"text,attr"`
	ResourceID    string    `xml:"resource-id,attr"`
	Class         string    `xml:"class,attr"`
	Package       string    `xml:"package,attr"`
	ContentDesc   string    `xml:"content-desc,attr"`
	Bounds        string    `xml:"bounds,attr"`
	Clickable     string    `xml:"clickable,attr"`
	Enabled       string    `xml:"enabled,attr"`
	Focusable     string    `xml:"focusable,attr"`
	Focused       string    `xml:"focused,attr"`
	Scrollable    string    `xml:"scrollable,attr"`
	LongClickable string    `xml:"long-clickable,attr"`
	Checkable     string    `xml:"checkable,attr"`
	Checked       string    `xml:"checked,attr"`
	Selected      string    `xml:"selected,attr"`
	Nodes         []XMLNode `xml:"node"`
}

// ElementQuery defines the locator criteria for finding UI elements.
type ElementQuery struct {
	ResourceID  string `yaml:"resourceId,omitempty"`
	ContentDesc string `yaml:"contentDesc,omitempty"`
	Text        string `yaml:"text,omitempty"`
	Class       string `yaml:"class,omitempty"`
	XPath       string `yaml:"xpath,omitempty"`
}

// Rect represents the bounds of a UI element.
type Rect struct {
	Left   int `yaml:"left" json:"left"`
	Top    int `yaml:"top" json:"top"`
	Right  int `yaml:"right" json:"right"`
	Bottom int `yaml:"bottom" json:"bottom"`
}

// UIElement represents a complete snapshot of a UI element/node.
type UIElement struct {
	Package       string       `yaml:"package,omitempty" json:"package"`
	ResourceID    string       `yaml:"resourceId,omitempty" json:"resourceId,omitempty"`
	Text          string       `yaml:"text,omitempty" json:"text,omitempty"`
	ContentDesc   string       `yaml:"contentDesc,omitempty" json:"contentDesc,omitempty"`
	Class         string       `yaml:"class,omitempty" json:"class"`
	Bounds        Rect         `yaml:"bounds" json:"bounds"`
	Clickable     bool         `yaml:"clickable" json:"clickable"`
	Enabled       bool         `yaml:"enabled" json:"enabled"`
	Focusable     bool         `yaml:"focusable" json:"focusable"`
	Focused       bool         `yaml:"focused" json:"focused"`
	Checked       bool         `yaml:"checked" json:"checked"`
	Selected      bool         `yaml:"selected" json:"selected"`
	LongClickable bool         `yaml:"longClickable,omitempty" json:"longClickable"`
	Checkable     bool         `yaml:"checkable,omitempty" json:"checkable"`
	Scrollable    bool         `yaml:"scrollable,omitempty" json:"scrollable"`
	Parent        *UIElement   `yaml:"-" json:"-"`
	Children      []*UIElement `yaml:"-" json:"children,omitempty"`
	XMLRef        *XMLNode     `yaml:"-" json:"-"`
}

// Script defines the top-level YAML automation script containing a sequence of steps.
type Script struct {
	Variables map[string]string `yaml:"variables,omitempty"`
	Steps     []Step            `yaml:"steps"`
}

// Step represents a single action step in the automation execution sequence.
type Step struct {
	Launch    *LaunchParams    `yaml:"launch,omitempty"`
	Terminate *TerminateParams `yaml:"terminate,omitempty"`
	Click     *ClickParams     `yaml:"click,omitempty"`
	Input     *InputParams     `yaml:"input,omitempty"`
	Swipe     *SwipeParams     `yaml:"swipe,omitempty"`
	Wait      *WaitParams      `yaml:"wait,omitempty"`
	Assert    *AssertParams    `yaml:"assert,omitempty"`

	If   *IfCondition `yaml:"if,omitempty"`
	Then []Step       `yaml:"then,omitempty"`
	Else []Step       `yaml:"else,omitempty"`

	DelayMs int `yaml:"delayMs,omitempty"`
}

// IfCondition defines the criteria for conditional checks (e.g. element existence).
type IfCondition struct {
	Exists *ExistsCondition `yaml:"exists,omitempty"`
}

// ExistsCondition defines the locator criteria to check if an element is present on screen.
type ExistsCondition struct {
	ResourceID  string `yaml:"resourceId,omitempty"`
	ContentDesc string `yaml:"contentDesc,omitempty"`
	Text        string `yaml:"text,omitempty"`
	Class       string `yaml:"class,omitempty"`
	XPath       string `yaml:"xpath,omitempty"`

	// New support: exists check by target UIElement
	Target *UIElement `yaml:"target,omitempty"`
}

// LaunchParams contains arguments for launching an app.
type LaunchParams struct {
	Package string `yaml:"package"`
}

// TerminateParams contains arguments for stopping an app.
type TerminateParams struct {
	Package string `yaml:"package"`
}

// Locator represents a single element-finding strategy with a confidence score.
type Locator struct {
	Strategy   string  `yaml:"strategy"`             // resourceId, text, contentDesc, xpath, coordinates
	Value      string  `yaml:"value"`                // the selector value
	Confidence int     `yaml:"confidence"`           // 0-100, higher = more reliable
	X          float64 `yaml:"x,omitempty"`          // only for strategy=coordinates
	Y          float64 `yaml:"y,omitempty"`          // only for strategy=coordinates
}

// AnchorContext provides disambiguation context.
type AnchorContext struct {
	SiblingText string `yaml:"siblingText,omitempty"`
	ParentID    string `yaml:"parentId,omitempty"`
	ParentClass string `yaml:"parentClass,omitempty"`
}

// ClickParams contains options for matching/locating the element to click.
type ClickParams struct {
	Target   *UIElement     `yaml:"target,omitempty"`
	Locators []Locator      `yaml:"locators,omitempty"`
	Anchor   *AnchorContext `yaml:"anchor,omitempty"`

	// Legacy fields
	ResourceID  string   `yaml:"resourceId,omitempty"`
	ContentDesc string   `yaml:"contentDesc,omitempty"`
	Text        string   `yaml:"text,omitempty"`
	Class       string   `yaml:"class,omitempty"`
	XPath       string   `yaml:"xpath,omitempty"`
	X           *float64 `yaml:"x,omitempty"`
	Y           *float64 `yaml:"y,omitempty"`
}

// InputParams contains options for typing text.
type InputParams struct {
	Text   string     `yaml:"text,omitempty"`
	Target *UIElement `yaml:"target,omitempty"`

	// Legacy fields
	Variable    string   `yaml:"variable,omitempty"`
	ResourceID  string   `yaml:"resourceId,omitempty"`
	ContentDesc string   `yaml:"contentDesc,omitempty"`
	Class       string   `yaml:"class,omitempty"`
	XPath       string   `yaml:"xpath,omitempty"`
	X           *float64 `yaml:"x,omitempty"`
	Y           *float64 `yaml:"y,omitempty"`
}

// SwipeParams contains coordinates and options for swipe gestures.
type SwipeParams struct {
	StartX     float64 `yaml:"startX"`
	StartY     float64 `yaml:"startY"`
	EndX       float64 `yaml:"endX"`
	EndY       float64 `yaml:"endY"`
	DurationMs int     `yaml:"durationMs,omitempty"`
}

// WaitParams contains parameters for waiting for specific UI elements/states.
type WaitParams struct {
	Target    *UIElement `yaml:"target,omitempty"`
	Condition string     `yaml:"condition"` // options: visible, hidden, present
	TimeoutMs int        `yaml:"timeoutMs,omitempty"`

	// Legacy fields
	ResourceID  string `yaml:"resourceId,omitempty"`
	ContentDesc string `yaml:"contentDesc,omitempty"`
	Text        string `yaml:"text,omitempty"`
	Class       string `yaml:"class,omitempty"`
	XPath       string `yaml:"xpath,omitempty"`
}

// AssertParams contains parameters for checking and validating UI element values/states.
type AssertParams struct {
	Target    *UIElement `yaml:"target,omitempty"`
	Condition string     `yaml:"condition"` // options: equals, contains, visible, hidden
	Value     string     `yaml:"value,omitempty"`
	TimeoutMs int        `yaml:"timeoutMs,omitempty"`

	// Legacy fields
	ResourceID  string `yaml:"resourceId,omitempty"`
	ContentDesc string `yaml:"contentDesc,omitempty"`
	Text        string `yaml:"text,omitempty"`
	Class       string `yaml:"class,omitempty"`
	XPath       string `yaml:"xpath,omitempty"`
}

// ParseScript decodes a YAML automation script from an io.Reader.
func ParseScript(r io.Reader) (*Script, error) {
	var s Script
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("failed to decode YAML automation script: %w", err)
	}
	return &s, nil
}

// ParseScriptFile opens a YAML script from disk and parses it.
func ParseScriptFile(path string) (*Script, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open script file: %w", err)
	}
	defer f.Close()
	return ParseScript(f)
}

// ToYAML encodes the Script struct into a YAML byte slice.
func (s *Script) ToYAML() ([]byte, error) {
	bytes, err := yaml.Marshal(s)
	if err != nil {
		return nil, err
	}
	// Post-process to unquote "y": key (YAML v2/v3 quotes 'y' because it is a boolean literal in YAML 1.1)
	yamlStr := strings.ReplaceAll(string(bytes), "\"y\":", "y:")
	yamlStr = strings.ReplaceAll(yamlStr, "'y':", "y:")
	return []byte(yamlStr), nil
}

// FlattenTree returns all elements in the tree in pre-order traversal.
func (el *UIElement) FlattenTree() []*UIElement {
	var elements []*UIElement
	var walk func(n *UIElement)
	walk = func(n *UIElement) {
		elements = append(elements, n)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(el)
	return elements
}
