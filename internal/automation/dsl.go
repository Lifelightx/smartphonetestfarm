package automation

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rect represents the bounds of a UI element.
type Rect struct {
	Left   int `yaml:"left"`
	Top    int `yaml:"top"`
	Right  int `yaml:"right"`
	Bottom int `yaml:"bottom"`
}

// UIElement represents a complete snapshot of a UI element/node.
type UIElement struct {
	Package     string `yaml:"package,omitempty"`
	ResourceID  string `yaml:"resourceId,omitempty"`
	Text        string `yaml:"text,omitempty"`
	ContentDesc string `yaml:"contentDesc,omitempty"`
	Class       string `yaml:"class,omitempty"`
	Bounds      Rect   `yaml:"bounds"`

	Clickable     bool `yaml:"clickable"`
	Enabled       bool `yaml:"enabled"`
	Focusable     bool `yaml:"focusable"`
	Focused       bool `yaml:"focused"`
	Checked       bool `yaml:"checked"`
	Selected      bool `yaml:"selected"`
	LongClickable bool `yaml:"longClickable,omitempty"`
	Checkable     bool `yaml:"checkable,omitempty"`
	Scrollable    bool `yaml:"scrollable,omitempty"`

	// Double-linked list pointers for memory traversal (ignored in YAML serialization)
	Parent   *UIElement   `yaml:"-"`
	Children []*UIElement `yaml:"-"`
	XMLRef   *XMLNode     `yaml:"-"`
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
