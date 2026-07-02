package automation

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
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

// ------------------------------------------------
// Node Scoring & Actionable Hit-Testing Engine
// ------------------------------------------------

// isScrollContainerClass returns true for container classes that scroll or hold lists.
func isScrollContainerClass(cls string) bool {
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

// isContainerClass returns true for layout/container classes that are almost never click targets.
func isContainerClass(cls string) bool {
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
func ScoreActionableNode(node *UIElement, screenWidth, screenHeight int32) int {
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
	if isScrollContainerClass(cls) {
		score -= 200 // Scroll containers are almost never the direct click target
	} else if isContainerClass(cls) {
		score -= 80  // Standard layouts
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
func ScoreNode(node *XMLNode, screenWidth, screenHeight int32) int {
	// Map XMLNode to UIElement to reuse the core scoring logic
	left, top, right, bottom, _ := parseBounds(node.Bounds)
	el := &UIElement{
		Class:       node.Class,
		Clickable:   node.Clickable == "true",
		Enabled:     node.Enabled == "true",
		ResourceID:  node.ResourceID,
		Text:        node.Text,
		ContentDesc: node.ContentDesc,
		Bounds: Rect{
			Left:   left,
			Top:    top,
			Right:  right,
			Bottom: bottom,
		},
	}
	return ScoreActionableNode(el, screenWidth, screenHeight)
}

// ------------------------------------------------
// Tree Construction & Parsing
// ------------------------------------------------

// ParseXMLTree parses a UiAutomator XML dump into a tree of UIElement objects.
func ParseXMLTree(xmlData string) (*UIElement, error) {
	var hierarchy UIHierarchy
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
	virtualRoot := &UIElement{
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
	virtualRoot.Bounds = Rect{Left: left, Top: top, Right: right, Bottom: bottom}
	return virtualRoot, nil
}

func convertXMLToUIElement(node *XMLNode, parent *UIElement) *UIElement {
	left, top, right, bottom, _ := parseBounds(node.Bounds)
	el := &UIElement{
		Package:     node.Package,
		ResourceID:  node.ResourceID,
		Text:        node.Text,
		ContentDesc: node.ContentDesc,
		Class:       node.Class,
		Bounds: Rect{
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

// ------------------------------------------------
// Hit-testing & Search Upwards (Point to Element)
// ------------------------------------------------

// FindBestElementAt coordinates hit-testing logic for a given point.
func FindBestElementAt(root *UIElement, x, y float64, width, height int32) *UIElement {
	px := int(x * float64(width))
	py := int(y * float64(height))

	var candidates []*UIElement
	
	// Helper to find all elements containing (px, py)
	var walk func(n *UIElement)
	walk = func(n *UIElement) {
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
	var actionableCandidates []*UIElement
	seen := make(map[*UIElement]bool)

	for _, cand := range candidates {
		curr := cand
		for curr != nil {
			isListItem := curr.Parent != nil && isScrollContainerClass(curr.Parent.Class)
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
func FindBestNodeAt(xmlData string, x, y float64, width, height int32) (*XMLNode, string) {
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

func buildXPathForElement(el *UIElement) string {
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

// ------------------------------------------------
// Dynamic Matcher & Scoring Engine
// ------------------------------------------------

// MatchScore calculates a matching score comparing a candidate element to a recorded target.
// MatchScore calculates a matching score comparing a candidate element to a recorded target.
func MatchScore(target *UIElement, candidate *UIElement, anchor *AnchorContext) int {
	score := 0

	// 1. ResourceID Match (+100 or -50 mismatch)
	if target.ResourceID != "" {
		if candidate.ResourceID != "" && (target.ResourceID == candidate.ResourceID || 
		   strings.HasSuffix(target.ResourceID, ":id/"+candidate.ResourceID) ||
		   strings.HasSuffix(candidate.ResourceID, ":id/"+target.ResourceID)) {
			score += 100
		} else {
			score -= 50
		}
	}

	// 2. Text Match (+80 or -50 mismatch)
	if target.Text != "" {
		if candidate.Text == target.Text {
			score += 80
		} else {
			score -= 50
		}
	}

	// 3. ContentDescription Match (+80 or -50 mismatch)
	if target.ContentDesc != "" {
		if candidate.ContentDesc == target.ContentDesc {
			score += 80
		} else {
			score -= 50
		}
	}

	// 4. Same Class Match (+20)
	if target.Class != "" && candidate.Class == target.Class {
		score += 20
	}

	// 5. Same Bounds Match (+30)
	if target.Bounds.Left == candidate.Bounds.Left &&
	   target.Bounds.Top == candidate.Bounds.Top &&
	   target.Bounds.Right == candidate.Bounds.Right &&
	   target.Bounds.Bottom == candidate.Bounds.Bottom {
		score += 30
	}

	// 6. Sibling / SiblingText Match (+50)
	if anchor != nil {
		if anchor.SiblingText != "" && candidate.Parent != nil {
			for _, sibling := range candidate.Parent.Children {
				if sibling != candidate && sibling.Text == anchor.SiblingText {
					score += 50
					break
				}
			}
		}
	} else if target.Parent != nil {
		for _, sibling := range target.Parent.Children {
			if sibling != target && sibling.Text != "" {
				if candidate.Parent != nil {
					for _, candSibling := range candidate.Parent.Children {
						if candSibling != candidate && candSibling.Text == sibling.Text {
							score += 50
							break
						}
					}
				}
			}
		}
	}

	// 7. Same Parent ID / Class Match (+40 for ID, +20 for class)
	if anchor != nil {
		if anchor.ParentClass != "" && candidate.Parent != nil {
			if candidate.Parent.Class == anchor.ParentClass {
				score += 20
			}
		}
		if anchor.ParentID != "" && candidate.Parent != nil {
			if candidate.Parent.ResourceID == anchor.ParentID ||
			   strings.HasSuffix(candidate.Parent.ResourceID, ":id/"+anchor.ParentID) ||
			   strings.HasSuffix(anchor.ParentID, ":id/"+candidate.Parent.ResourceID) {
				score += 40
			}
		}
	} else if target.Parent != nil && candidate.Parent != nil {
		if target.Parent.Class == candidate.Parent.Class {
			score += 30
		}
	}

	return score
}

// ResolveElement finds the element in the current live tree that best matches the target.
func ResolveElement(liveTree *UIElement, target *UIElement, anchor *AnchorContext) (*UIElement, int, error) {
	if liveTree == nil || target == nil {
		return nil, 0, fmt.Errorf("nil liveTree or target element")
	}

	candidates := liveTree.FlattenTree()
	if len(candidates) == 0 {
		return nil, 0, fmt.Errorf("no elements in live tree")
	}

	type matchCandidate struct {
		node  *UIElement
		score int
	}

	var matches []matchCandidate
	for _, cand := range candidates {
		score := MatchScore(target, cand, anchor)
		if score > 0 {
			matches = append(matches, matchCandidate{node: cand, score: score})
		}
	}

	if len(matches) == 0 {
		return nil, 0, fmt.Errorf("no matches found for target element")
	}

	// Sort matches by score descending, tie-break by smaller area difference
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		targetArea := (target.Bounds.Right - target.Bounds.Left) * (target.Bounds.Bottom - target.Bounds.Top)
		areaI := (matches[i].node.Bounds.Right - matches[i].node.Bounds.Left) * (matches[i].node.Bounds.Bottom - matches[i].node.Bounds.Top)
		areaJ := (matches[j].node.Bounds.Right - matches[j].node.Bounds.Left) * (matches[j].node.Bounds.Bottom - matches[j].node.Bounds.Top)
		diffI := abs(areaI - targetArea)
		diffJ := abs(areaJ - targetArea)
		return diffI < diffJ
	})

	bestMatch := matches[0]
	// Require positive match score (minimum 20: same class or bounds or ID/text match)
	if bestMatch.score < 20 {
		return nil, 0, fmt.Errorf("best match score %d is below threshold", bestMatch.score)
	}

	return bestMatch.node, bestMatch.score, nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ------------------------------------------------
// Sibling/Parent Context Extraction
// ------------------------------------------------

// ExtractAnchor extracts sibling text and parent context for disambiguation.
func ExtractAnchor(xmlData string, targetNode *XMLNode) *AnchorContext {
	if targetNode == nil {
		return nil
	}

	var hierarchy UIHierarchy
	if err := xml.Unmarshal([]byte(xmlData), &hierarchy); err != nil {
		return nil
	}

	anchor := &AnchorContext{}
	findParentAndSiblings(&hierarchy, targetNode, anchor)

	if anchor.SiblingText == "" && anchor.ParentID == "" {
		return nil
	}
	return anchor
}

func findParentAndSiblings(h *UIHierarchy, target *XMLNode, anchor *AnchorContext) bool {
	for i := range h.Nodes {
		if findParentAndSiblingsInNode(&h.Nodes[i], target, anchor) {
			return true
		}
	}
	return false
}

func findParentAndSiblingsInNode(parent *XMLNode, target *XMLNode, anchor *AnchorContext) bool {
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

func findFirstTextNode(node *XMLNode) *XMLNode {
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

// ------------------------------------------------
// Locator Generation with Confidence Scores
// ------------------------------------------------

// GenerateLocators produces a ranked list of locator strategies for a target node.
func GenerateLocators(xmlData string, targetNode *XMLNode, x, y float64) []Locator {
	if targetNode == nil {
		return []Locator{{Strategy: "coordinates", Value: fmt.Sprintf("%.6f,%.6f", x, y), Confidence: 10, X: x, Y: y}}
	}

	var hierarchy UIHierarchy
	if err := xml.Unmarshal([]byte(xmlData), &hierarchy); err != nil {
		return []Locator{{Strategy: "coordinates", Value: fmt.Sprintf("%.6f,%.6f", x, y), Confidence: 10, X: x, Y: y}}
	}

	var locators []Locator

	if targetNode.ResourceID != "" {
		isContainer := isContainerClass(targetNode.Class)
		hasIdentity := targetNode.Text != "" || targetNode.ContentDesc != ""
		count := countMatching(&hierarchy, func(n *XMLNode) bool {
			return n.ResourceID == targetNode.ResourceID
		})
		switch {
		case isContainer && !hasIdentity:
			locators = append(locators, Locator{Strategy: "resourceId", Value: targetNode.ResourceID, Confidence: 20})
		case count == 1:
			locators = append(locators, Locator{Strategy: "resourceId", Value: targetNode.ResourceID, Confidence: 100})
		default:
			locators = append(locators, Locator{Strategy: "resourceId", Value: targetNode.ResourceID, Confidence: 40})
		}
	}

	if targetNode.Text != "" {
		count := countMatching(&hierarchy, func(n *XMLNode) bool {
			return n.Text == targetNode.Text
		})
		if count == 1 {
			locators = append(locators, Locator{Strategy: "text", Value: targetNode.Text, Confidence: 95})
		} else {
			locators = append(locators, Locator{Strategy: "text", Value: targetNode.Text, Confidence: 60})
		}
	}

	if targetNode.ContentDesc != "" {
		count := countMatching(&hierarchy, func(n *XMLNode) bool {
			return n.ContentDesc == targetNode.ContentDesc
		})
		if count == 1 {
			locators = append(locators, Locator{Strategy: "contentDesc", Value: targetNode.ContentDesc, Confidence: 90})
		} else {
			locators = append(locators, Locator{Strategy: "contentDesc", Value: targetNode.ContentDesc, Confidence: 55})
		}
	}

	if targetNode.Class != "" {
		locators = append(locators, Locator{
			Strategy:   "class",
			Value:      targetNode.Class,
			Confidence: 30,
		})
	}

	// Generate XPath locator
	var findXPath func(n *XMLNode, target *XMLNode, path string) (string, bool)
	findXPath = func(n *XMLNode, target *XMLNode, path string) (string, bool) {
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
		locators = append(locators, Locator{
			Strategy:   "xpath",
			Value:      xpathVal,
			Confidence: 20,
		})
	}

	locators = append(locators, Locator{Strategy: "coordinates", Value: fmt.Sprintf("%.6f,%.6f", x, y), Confidence: 10, X: x, Y: y})

	sort.Slice(locators, func(i, j int) bool {
		return locators[i].Confidence > locators[j].Confidence
	})

	return locators
}

func countMatching(h *UIHierarchy, pred func(*XMLNode) bool) int {
	count := 0
	var walk func(n *XMLNode)
	walk = func(n *XMLNode) {
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

// ------------------------------------------------
// Replay-side: Find element by Locator (routes through ResolveElement)
// ------------------------------------------------

// FindByLocator searches the UI hierarchy for a node matching the given locator strategy.
func FindByLocator(xmlData string, loc Locator, anchor *AnchorContext) (*XMLNode, error) {
	root, err := ParseXMLTree(xmlData)
	if err != nil {
		return nil, err
	}

	// Create temporary target element
	target := &UIElement{}
	switch loc.Strategy {
	case "resourceId":
		target.ResourceID = loc.Value
	case "text":
		target.Text = loc.Value
	case "contentDesc":
		target.ContentDesc = loc.Value
	case "class":
		target.Class = loc.Value
	case "xpath":
		var hierarchy UIHierarchy
		if errXml := xml.Unmarshal([]byte(xmlData), &hierarchy); errXml == nil {
			match := EvaluateXPath(&hierarchy, loc.Value)
			if match != nil {
				return match, nil
			}
		}
		return nil, fmt.Errorf("xpath locator matched nothing: %s", loc.Value)
	default:
		return nil, fmt.Errorf("unknown locator strategy: %s", loc.Strategy)
	}

	bestMatch, _, err := ResolveElement(root, target, anchor)
	if err != nil {
		return nil, err
	}

	if bestMatch.XMLRef == nil {
		return nil, fmt.Errorf("no matching XMLRef node found")
	}

	return bestMatch.XMLRef, nil
}

// ------------------------------------------------
// Legacy support: searchNode, FindElement, etc.
// ------------------------------------------------

func FindElement(xmlData string, query ElementQuery, width, height int32) (float64, float64, error) {
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid screen dimensions: %dx%d", width, height)
	}

	root, err := ParseXMLTree(xmlData)
	if err != nil {
		return 0, 0, err
	}

	target := &UIElement{
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

func searchNode(node *XMLNode, query ElementQuery) *XMLNode {
	if matchesQuery(node, query) {
		return node
	}
	for i := range node.Nodes {
		if found := searchNode(&node.Nodes[i], query); found != nil {
			return found
		}
	}
	return nil
}

func matchesQuery(node *XMLNode, query ElementQuery) bool {
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

func parseBounds(boundsStr string) (left, top, right, bottom int, err error) {
	_, err = fmt.Sscanf(boundsStr, "[%d,%d][%d,%d]", &left, &top, &right, &bottom)
	return
}

func FindFocusedOrEditTextNode(xmlData string) (*XMLNode, error) {
	var hierarchy UIHierarchy
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

func findNodeByFocus(node *XMLNode) *XMLNode {
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

func findNodeByClass(node *XMLNode, className string) *XMLNode {
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

// EvaluateXPath parses and evaluates simple and indexed XPath expressions on a UI hierarchy.
func EvaluateXPath(hierarchy *UIHierarchy, xpath string) *XMLNode {
	if xpath == "" {
		return nil
	}

	xpath = strings.TrimSpace(xpath)

	targetIdx := 1
	if strings.HasPrefix(xpath, "(") {
		lastClose := strings.LastIndex(xpath, ")")
		if lastClose != -1 && lastClose < len(xpath)-1 {
			suffix := xpath[lastClose+1:]
			if strings.HasPrefix(suffix, "[") && strings.HasSuffix(suffix, "]") {
				var parsedIdx int
				if _, err := fmt.Sscanf(suffix[1:len(suffix)-1], "%d", &parsedIdx); err == nil {
					targetIdx = parsedIdx
					xpath = xpath[1:lastClose]
				}
			}
		}
	}

	if strings.HasPrefix(xpath, "/hierarchy/") {
		parts := strings.Split(strings.TrimPrefix(xpath, "/hierarchy/"), "/")
		currentNodes := hierarchy.Nodes
		var matchedNode *XMLNode

		for pIdx, part := range parts {
			if !strings.HasPrefix(part, "node[@index='") || !strings.HasSuffix(part, "']") {
				return nil
			}
			idxStr := part[len("node[@index='") : len(part)-len("']")]

			var found *XMLNode
			for i := range currentNodes {
				if currentNodes[i].Index == idxStr {
					found = &currentNodes[i]
					break
				}
			}
			if found == nil {
				return nil
			}
			matchedNode = found
			if pIdx < len(parts)-1 {
				currentNodes = found.Nodes
			}
		}
		return matchedNode
	}

	if strings.HasPrefix(xpath, "//") {
		xpathSuffix := strings.TrimPrefix(xpath, "//")
		bracketIdx := strings.Index(xpathSuffix, "[")

		var className string
		var attrName string
		var attrVal string
		hasAttr := false

		if bracketIdx == -1 {
			className = xpathSuffix
		} else {
			className = xpathSuffix[:bracketIdx]
			attrExpr := xpathSuffix[bracketIdx+1 : len(xpathSuffix)-1]
			if strings.HasPrefix(attrExpr, "@") {
				attrParts := strings.SplitN(strings.TrimPrefix(attrExpr, "@"), "=", 2)
				if len(attrParts) == 2 {
					attrName = strings.TrimSpace(attrParts[0])
					attrVal = strings.Trim(strings.TrimSpace(attrParts[1]), "'\"")
					hasAttr = true
				}
			}
		}

		var matches []*XMLNode
		collectAll(hierarchy, func(n *XMLNode) bool {
			if className != "" && className != "*" && className != "node" && n.Class != className {
				return false
			}
			if hasAttr {
				switch attrName {
				case "resource-id":
					return n.ResourceID == attrVal || strings.HasSuffix(n.ResourceID, ":id/"+attrVal)
				case "content-desc":
					return n.ContentDesc == attrVal
				case "text":
					return n.Text == attrVal || strings.Contains(n.Text, attrVal)
				case "class":
					return n.Class == attrVal
				case "index":
					return n.Index == attrVal
				default:
					return false
				}
			}
			return true
		}, &matches)

		if targetIdx >= 1 && targetIdx <= len(matches) {
			return matches[targetIdx-1]
		}
		return nil
	}

	return nil
}

func collectAll(h *UIHierarchy, pred func(*XMLNode) bool, result *[]*XMLNode) {
	for i := range h.Nodes {
		collectAllInNode(&h.Nodes[i], pred, result)
	}
}

func collectAllInNode(n *XMLNode, pred func(*XMLNode) bool, result *[]*XMLNode) {
	if pred(n) {
		*result = append(*result, n)
	}
	for i := range n.Nodes {
		collectAllInNode(&n.Nodes[i], pred, result)
	}
}
