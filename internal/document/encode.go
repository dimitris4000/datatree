package document

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

func (d *Document) JSON(id string, allowLoss bool) (string, error) {
	n := d.Nodes[id]
	if n == nil {
		return "", errors.New("node not found")
	}
	b, err := jsonBytes(n.raw, allowLoss)
	if err != nil {
		return "", err
	}
	var pretty bytes.Buffer
	if err = json.Indent(&pretty, b, "", "  "); err != nil {
		return "", err
	}
	return pretty.String(), nil
}
func jsonBytes(n *yaml.Node, allowLoss bool) ([]byte, error) {
	var out bytes.Buffer
	visiting := map[*yaml.Node]bool{}
	count := 0
	merges := newMergeResolver()
	var write func(*yaml.Node, int) error
	write = func(n *yaml.Node, depth int) error {
		count++
		if count > MaxNodes || depth > MaxDepth || out.Len() > MaxBytes {
			return errors.New("conversion exceeds document limits")
		}
		if visiting[n] {
			return errors.New("cyclic aliases cannot be represented as JSON")
		}
		visiting[n] = true
		defer delete(visiting, n)
		if !allowLoss && (n.Anchor != "" || n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" || n.Kind == yaml.AliasNode || n.Style&yaml.TaggedStyle != 0) {
			return errors.New("JSON conversion loses YAML comments, tags, or references; preview and confirm conversion")
		}
		switch n.Kind {
		case yaml.AliasNode:
			if n.Alias == nil {
				return errors.New("dangling alias")
			}
			return write(n.Alias, depth+1)
		case yaml.MappingNode:
			if !allowLoss {
				for i := 0; i < len(n.Content); i += 2 {
					k := n.Content[i]
					if k.Tag == "!!merge" || k.HeadComment != "" || k.LineComment != "" || k.FootComment != "" || k.Style&yaml.TaggedStyle != 0 {
						return errors.New("JSON conversion loses YAML mapping metadata; preview and confirm conversion")
					}
				}
			}
			entries, err := merges.entries(n)
			if err != nil {
				return err
			}
			out.WriteByte('{')
			for i, pair := range entries {
				if pair[0].Kind != yaml.ScalarNode || pair[0].Tag != "!!str" {
					return errors.New("non-string YAML keys cannot be converted safely")
				}
				if i > 0 {
					out.WriteByte(',')
				}
				key, _ := json.Marshal(pair[0].Value)
				out.Write(key)
				out.WriteByte(':')
				if err := write(pair[1], depth+1); err != nil {
					return err
				}
			}
			out.WriteByte('}')
		case yaml.SequenceNode, yaml.DocumentNode:
			out.WriteByte('[')
			for i, c := range n.Content {
				if i > 0 {
					out.WriteByte(',')
				}
				if err := write(c, depth+1); err != nil {
					return err
				}
			}
			out.WriteByte(']')
		default:
			switch n.Tag {
			case "!!str", "!!timestamp", "!!binary":
				if n.Tag != "!!str" && !allowLoss {
					return errors.New("YAML scalar type requires explicit conversion")
				}
				v, _ := json.Marshal(n.Value)
				out.Write(v)
			case "!!null":
				out.WriteString("null")
			case "!!bool":
				switch strings.ToLower(n.Value) {
				case "true", "yes", "on":
					out.WriteString("true")
				default:
					out.WriteString("false")
				}
			case "!!int":
				v := strings.ReplaceAll(n.Value, "_", "")
				if json.Valid([]byte(v)) {
					out.WriteString(v)
				} else {
					z, ok := new(big.Int).SetString(v, 0)
					if !ok {
						return errors.New("invalid integer")
					}
					out.WriteString(z.String())
				}
			case "!!float":
				v := strings.ReplaceAll(n.Value, "_", "")
				if json.Valid([]byte(v)) {
					out.WriteString(v)
				} else {
					f, err := strconv.ParseFloat(v, 64)
					if err != nil {
						return errors.New("nonfinite or unsupported YAML number")
					}
					s := strconv.FormatFloat(f, 'g', -1, 64)
					if !json.Valid([]byte(s)) {
						return errors.New("nonfinite YAML number")
					}
					out.WriteString(s)
				}
			default:
				if !allowLoss {
					return errors.New("custom YAML tag requires explicit conversion")
				}
				v, _ := json.Marshal(n.Value)
				out.Write(v)
			}
		}
		return nil
	}
	if err := write(n, 0); err != nil {
		return nil, err
	}
	if out.Len() > MaxBytes {
		return nil, errors.New("conversion exceeds 10 MB")
	}
	return out.Bytes(), nil
}

type mergeResolver struct {
	seen map[*yaml.Node]bool
	memo map[*yaml.Node][][2]*yaml.Node
	work int
}

func newMergeResolver() *mergeResolver {
	return &mergeResolver{seen: map[*yaml.Node]bool{}, memo: map[*yaml.Node][][2]*yaml.Node{}}
}
func (r *mergeResolver) entries(n *yaml.Node) ([][2]*yaml.Node, error) {
	r.work++
	if r.work > MaxNodes {
		return nil, errors.New("merge expansion exceeds document work limit")
	}
	if n == nil {
		return nil, errors.New("dangling merge alias")
	}
	if v, ok := r.memo[n]; ok {
		return v, nil
	}
	if r.seen[n] {
		return nil, errors.New("cyclic merge")
	}
	r.seen[n] = true
	defer delete(r.seen, n)
	if n.Kind == yaml.AliasNode {
		v, err := r.entries(n.Alias)
		if err == nil {
			r.memo[n] = v
		}
		return v, err
	}
	if n.Kind != yaml.MappingNode {
		return nil, errors.New("merge target must be a mapping")
	}
	var pairs [][2]*yaml.Node
	used := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Tag != "!!merge" {
			pairs = append(pairs, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
			used[n.Content[i].Value] = true
			r.work++
		}
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Tag != "!!merge" {
			continue
		}
		merge := n.Content[i+1]
		list := []*yaml.Node{merge}
		if merge.Kind == yaml.SequenceNode {
			list = merge.Content
		}
		for _, m := range list {
			entries, err := r.entries(m)
			if err != nil {
				return nil, err
			}
			for _, p := range entries {
				r.work++
				if r.work > MaxNodes {
					return nil, errors.New("merge expansion exceeds document work limit")
				}
				if !used[p[0].Value] {
					pairs = append(pairs, p)
					used[p[0].Value] = true
				}
			}
		}
	}
	r.memo[n] = pairs
	return pairs, nil
}

func (d *Document) YAML(id string) (string, error) {
	n := d.Nodes[id]
	if n == nil {
		return "", errors.New("node not found")
	}
	if d.Format == "yaml" && n.End > n.Start {
		return dedentFragment(string(d.Source[n.Start:n.End]), fragmentIndent(n)), nil
	}
	var b []byte
	var err error
	if d.Format == "yaml" {
		b, err = marshalYAML(n.raw)
	} else {
		b, err = marshalBlockYAML(n.raw)
	}
	return string(b), err
}

// marshalBlockYAML removes imported presentation styles without changing the
// document's nodes, scalar tags, values, or mapping order.
func marshalBlockYAML(n *yaml.Node) ([]byte, error) {
	table := map[*yaml.Node]*yaml.Node{}
	root := clone(n, table)
	for _, c := range table {
		c.Style = 0
	}
	return marshalYAML(root)
}

// conversionBytes leaves ordinary YAML serialization and edits style-preserving.
func (d *Document) conversionBytes(format string) ([]byte, error) {
	if format == "yaml" && d.Format != "yaml" {
		return marshalBlockYAML(d.raw)
	}
	return encode(format, d.raw, nil)
}

func marshalYAML(n *yaml.Node) ([]byte, error) {
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if n.Kind == yaml.DocumentNode {
		for _, c := range n.Content {
			if err := e.Encode(c); err != nil {
				return nil, err
			}
		}
	} else {
		if err := e.Encode(n); err != nil {
			return nil, err
		}
	}
	err := e.Close()
	return b.Bytes(), err
}
func encode(format string, n *yaml.Node, headers []string) ([]byte, error) {
	switch format {
	case "yaml":
		return marshalYAML(n)
	case "json":
		b, err := jsonBytes(n, true)
		if err != nil {
			return nil, err
		}
		var p bytes.Buffer
		if err = json.Indent(&p, b, "", "  "); err != nil {
			return nil, err
		}
		p.WriteByte('\n')
		return p.Bytes(), nil
	case "jsonl":
		if n.Kind != yaml.SequenceNode {
			return nil, errors.New("JSONL root must remain an array")
		}
		var b bytes.Buffer
		for _, c := range n.Content {
			j, err := jsonBytes(c, true)
			if err != nil {
				return nil, err
			}
			b.Write(j)
			b.WriteByte('\n')
		}
		return b.Bytes(), nil
	case "csv":
		if n.Kind != yaml.SequenceNode {
			return nil, errors.New("CSV root must remain an array of rows")
		}
		var b bytes.Buffer
		w := csv.NewWriter(&b)
		if err := w.Write(headers); err != nil {
			return nil, err
		}
		for _, row := range n.Content {
			if row.Kind != yaml.MappingNode || len(row.Content) != len(headers)*2 {
				return nil, errors.New("CSV rows must match the original headers")
			}
			cells := make([]string, len(headers))
			for i, h := range headers {
				if row.Content[i*2].Value != h || row.Content[i*2+1].Kind != yaml.ScalarNode || row.Content[i*2+1].Tag != "!!str" {
					return nil, errors.New("CSV cells must be strings and headers must remain unchanged")
				}
				cells[i] = row.Content[i*2+1].Value
			}
			if err := w.Write(cells); err != nil {
				return nil, err
			}
		}
		w.Flush()
		return b.Bytes(), w.Error()
	}
	return nil, fmt.Errorf("unknown format %s", format)
}
