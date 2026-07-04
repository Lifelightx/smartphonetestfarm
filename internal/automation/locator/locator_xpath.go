package locator

import (
	"fmt"
	"strings"

	"protean-provider/internal/automation/dsl"
)

// EvaluateXPath parses and evaluates simple and indexed XPath expressions on a UI hierarchy.
func EvaluateXPath(hierarchy *dsl.UIHierarchy, xpath string) *dsl.XMLNode {
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
		var matchedNode *dsl.XMLNode

		for pIdx, part := range parts {
			if !strings.HasPrefix(part, "node[@index='") || !strings.HasSuffix(part, "']") {
				return nil
			}
			idxStr := part[len("node[@index='") : len(part)-len("']")]

			var found *dsl.XMLNode
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

		var matches []*dsl.XMLNode
		collectAll(hierarchy, func(n *dsl.XMLNode) bool {
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

func collectAll(h *dsl.UIHierarchy, pred func(*dsl.XMLNode) bool, result *[]*dsl.XMLNode) {
	for i := range h.Nodes {
		collectAllInNode(&h.Nodes[i], pred, result)
	}
}

func collectAllInNode(n *dsl.XMLNode, pred func(*dsl.XMLNode) bool, result *[]*dsl.XMLNode) {
	if pred(n) {
		*result = append(*result, n)
	}
	for i := range n.Nodes {
		collectAllInNode(&n.Nodes[i], pred, result)
	}
}
