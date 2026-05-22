package ruler

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/btcsuite/btcutil/base58"
	"github.com/goccy/go-yaml/ast"
	yast "github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"gopkg.in/yaml.v3"
)

const (
	kwHash  = "hash"
	kwTerms = "terms"
	kwRules = "rules"
	kwMeta  = "metadata"
)

// Parse rule data into into N rules and hash content.
// Return generic structure with hashes injected for each rule.
func HashRules(data []byte) ([]map[string]any, error) {

	// First parse the YAML into a generic structure
	var root map[string]any

	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	rules, ok := root[kwRules].([]any)
	if !ok {
		return nil, fmt.Errorf("expected 'rules' key to be a sequence")
	}

	// Iterate across the rules, treating each as a separate document.

	rList := make([]map[string]any, 0, len(rules))

	for _, r := range rules {

		rr, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected rule to be a mapping")
		}

		meta, ok := rr[kwMeta].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected metadata to be a mapping")
		}

		delete(meta, kwHash)

		nRoot := map[string]any{
			kwRules: []any{rr},
		}

		// Remarshal the rule with the hash removed to get a stable representation for hashing.
		ruleBytes, err := yaml.Marshal(nRoot)
		if err != nil {
			return nil, err
		}

		hash := hashFunc(ruleBytes)
		meta[kwHash] = hash

		rList = append(rList, nRoot)
	}

	return rList, nil
}

func hashFunc(data []byte) string {
	sum := sha256.Sum256(data)
	return base58.Encode(sum[:])

}

func generateSrc(node ast.Node) ([]byte, error) {
	if node == nil {
		return nil, nil
	}

	mNode, ok := node.(*yast.SequenceNode)
	if !ok {
		return nil, fmt.Errorf("expected sequence node, got %T", node)
	}

	// Have to get the alignment right or the merge will may strip
	// the front of multi-line literals.  Yaml is a pain.
	col := 2
	if mNode.Start.Position.Column < 3 {
		col -= 3 - mNode.Start.Position.Column
	}

	// Add artificial root key
	rootSrc := fmt.Sprintf("%s:\n%s[]", kwRules, strings.Repeat(" ", col))
	file, err := parser.ParseBytes([]byte(rootSrc), 0)
	if err != nil {
		return nil, err
	}

	doc := file.Docs[0]
	root, ok := doc.Body.(*ast.MappingNode)
	if !ok {
		return nil, fmt.Errorf("expected mapping node, got %T", doc.Body)
	}

	if len(root.Values) != 1 {
		return nil, fmt.Errorf("expected root mapping to have exactly one key, got %d", len(root.Values))
	}

	child, ok := root.Values[0].Value.(*ast.SequenceNode)
	if !ok {
		return nil, fmt.Errorf("expected sequence node, got %T", root.Values[0].Value)
	}
	child.SetIsFlowStyle(false)

	child.Merge(mNode)

	v := root.String()

	return []byte(v), nil
}
