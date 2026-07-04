package locator

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"protean-provider/internal/automation/dsl"
)

// MatchScore calculates a matching score comparing a candidate element to a recorded target.
func MatchScore(target *dsl.UIElement, candidate *dsl.UIElement, anchor *dsl.AnchorContext) int {
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
func ResolveElement(liveTree *dsl.UIElement, target *dsl.UIElement, anchor *dsl.AnchorContext) (*dsl.UIElement, int, error) {
	if liveTree == nil || target == nil {
		return nil, 0, fmt.Errorf("nil liveTree or target element")
	}

	candidates := liveTree.FlattenTree()
	if len(candidates) == 0 {
		return nil, 0, fmt.Errorf("no elements in live tree")
	}

	type matchCandidate struct {
		node  *dsl.UIElement
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

// FindByLocator searches the UI hierarchy for a node matching the given locator strategy.
func FindByLocator(xmlData string, loc dsl.Locator, anchor *dsl.AnchorContext) (*dsl.XMLNode, error) {
	root, err := ParseXMLTree(xmlData)
	if err != nil {
		return nil, err
	}

	// Create temporary target element
	target := &dsl.UIElement{}
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
		var hierarchy dsl.UIHierarchy
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
