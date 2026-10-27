package document

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type schemaInstance struct {
	value any
	nodes map[string]*Node
	root  *Node
	index int
}
type schemaProjection struct {
	rawNodes    map[*yaml.Node]*Node
	visiting    map[*yaml.Node]bool
	merges      *mergeResolver
	work, bytes int
	target      *Node
	limits      Limits
}

func pointer(parts []string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(p, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

// The engine uses exact rationals. Bound exponent expansion before handing it
// numbers: an exponent with thousands of digits must not allocate 10^exponent.
func schemaNumber(n *Node) (json.Number, error) {
	text, ok := queryNumberText(n)
	if !ok || len(text) > 4096 {
		return "", errors.New("unsupported or oversized number")
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, ok := new(big.Int).SetString(text[i+1:], 10)
		if !ok || new(big.Int).Abs(exponent).Cmp(big.NewInt(4096)) > 0 {
			return "", errors.New("number exponent exceeds validation limit (4096)")
		}
	}
	decimal, ok := decimalNumber(text)
	if !ok {
		return "", errors.New("nonfinite or unsupported YAML number")
	}
	if new(big.Int).Abs(decimal.magnitude).Cmp(big.NewInt(4096)) > 0 {
		return "", errors.New("number magnitude exceeds validation limit (4096 decimal places)")
	}
	return json.Number(text), nil
}

func (p *schemaProjection) project(n *yaml.Node, target, forced *Node, path []string, depth int, instance *schemaInstance) (any, error) {
	p.work++
	p.target = target
	if forced != nil {
		target = forced
		p.target = forced
	}
	if n == nil {
		return nil, errors.New("dangling alias")
	}
	if p.work > p.limits.MaxNodes || depth > MaxDepth {
		return nil, errors.New("expanded document exceeds validation node/depth limits")
	}
	p.bytes += len(n.Value) + 8
	if p.bytes > p.limits.MaxBytes {
		return nil, errors.New("expanded document exceeds validation byte limit")
	}
	if p.visiting[n] {
		return nil, errors.New("cyclic YAML references cannot be validated")
	}
	p.visiting[n] = true
	defer delete(p.visiting, n)
	instance.nodes[pointer(path)] = target
	if n.Kind == yaml.AliasNode {
		return p.project(n.Alias, target, target, path, depth+1, instance)
	}
	switch n.Kind {
	case yaml.MappingNode:
		if n.Tag != "!!map" {
			return nil, errors.New("custom YAML tags cannot be validated")
		}
		local := map[*yaml.Node]bool{}
		var mergeNodes []*Node
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || (key.Tag != "!!str" && key.Tag != "!!merge") {
				return nil, errors.New("non-string or complex YAML keys cannot be validated")
			}
			if key.Tag == "!!merge" {
				if err := p.checkMergeSource(n.Content[i+1], map[*yaml.Node]bool{}); err != nil {
					if forced == nil {
						p.target = p.rawNodes[n.Content[i+1]]
					}
					return nil, err
				}
				mergeNodes = append(mergeNodes, p.rawNodes[n.Content[i+1]])
			} else {
				local[key] = true
			}
		}
		entries, err := p.merges.entries(n)
		if err != nil {
			return nil, err
		}
		value := map[string]any{}
		for _, pair := range entries {
			if pair[0].Kind != yaml.ScalarNode || pair[0].Tag != "!!str" {
				return nil, errors.New("non-string or complex YAML keys cannot be validated")
			}
			childTarget := p.rawNodes[pair[1]]
			childForced := forced
			if !local[pair[0]] {
				for _, merge := range mergeNodes {
					raw := merge.raw
					list := []*yaml.Node{raw}
					if raw.Kind == yaml.SequenceNode {
						list = raw.Content
					}
					matched := false
					for _, m := range list {
						pairs, e := p.merges.entries(m)
						if e != nil {
							return nil, e
						}
						for _, v := range pairs {
							if v[0] == pair[0] {
								matched = true
								break
							}
						}
						if matched {
							break
						}
					}
					if matched {
						childTarget = merge
						if childForced == nil {
							childForced = merge
						}
						break
					}
				}
			}
			if childTarget == nil {
				childTarget = target
			}
			next := append(append([]string(nil), path...), pair[0].Value)
			v, err := p.project(pair[1], childTarget, childForced, next, depth+1, instance)
			if err != nil {
				return nil, err
			}
			value[pair[0].Value] = v
		}
		return value, nil
	case yaml.SequenceNode:
		if n.Tag != "!!seq" {
			return nil, errors.New("custom YAML tags cannot be validated")
		}
		value := make([]any, len(n.Content))
		for i, c := range n.Content {
			child := p.rawNodes[c]
			if child == nil {
				child = target
			}
			next := append(append([]string(nil), path...), strconv.Itoa(i))
			v, err := p.project(c, child, forced, next, depth+1, instance)
			if err != nil {
				return nil, err
			}
			value[i] = v
		}
		return value, nil
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!str", "!!timestamp", "!!binary":
			return n.Value, nil
		case "!!null":
			var value any
			if err := n.Decode(&value); err != nil {
				return nil, errors.New("invalid YAML null scalar")
			}
			return value, nil
		case "!!bool":
			var value bool
			if err := n.Decode(&value); err != nil {
				return nil, errors.New("invalid YAML boolean scalar")
			}
			return value, nil
		case "!!int", "!!float":
			return schemaNumber(&Node{Value: n.Value, raw: n})
		default:
			return nil, errors.New("custom YAML tags cannot be validated")
		}
	}
	return nil, fmt.Errorf("unsupported YAML node kind %d", n.Kind)
}

func (p *schemaProjection) checkMergeSource(n *yaml.Node, visiting map[*yaml.Node]bool) error {
	p.work++
	if p.work > p.limits.MaxNodes {
		return errors.New("merge expansion exceeds validation work limit")
	}
	if n == nil {
		return errors.New("dangling merge alias")
	}
	if visiting[n] {
		return errors.New("cyclic YAML merge cannot be validated")
	}
	visiting[n] = true
	defer delete(visiting, n)
	if n.Kind == yaml.AliasNode {
		return p.checkMergeSource(n.Alias, visiting)
	}
	if n.Kind == yaml.SequenceNode {
		if n.Tag != "!!seq" {
			return errors.New("custom YAML tags cannot be validated")
		}
		for _, c := range n.Content {
			if err := p.checkMergeSource(c, visiting); err != nil {
				return err
			}
		}
		return nil
	}
	if n.Kind != yaml.MappingNode || n.Tag != "!!map" {
		return errors.New("merge source must be an ordinary YAML mapping")
	}
	for i := 0; i < len(n.Content); i += 2 {
		key := n.Content[i]
		if key.Tag == "!!merge" {
			if err := p.checkMergeSource(n.Content[i+1], visiting); err != nil {
				return err
			}
		} else if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return errors.New("non-string or complex YAML merge keys cannot be validated")
		}
	}
	return nil
}
