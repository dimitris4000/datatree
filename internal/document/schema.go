package document

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	js "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const MaxSchemaBytes = 1024 * 1024

type CompiledSchema struct {
	schema    *js.Schema
	locations map[string]*js.Schema
}
type ValidationMark struct {
	Direct   bool `json:"direct,omitempty"`
	Contains bool `json:"contains,omitempty"`
}
type ValidationDiagnostic struct {
	Target               string   `json:"target"`
	InstancePath         string   `json:"instancePath"`
	DocumentIndex        int      `json:"documentIndex"`
	Location             string   `json:"location"`
	Keyword              string   `json:"keyword"`
	SchemaPath           string   `json:"schemaPath,omitempty"`
	Message              string   `json:"message"`
	Suggestions          []string `json:"suggestions,omitempty"`
	SuggestionsTruncated bool     `json:"suggestionsTruncated,omitempty"`
}
type ValidationResult struct {
	Valid       bool                      `json:"valid"`
	Complete    bool                      `json:"complete"`
	Diagnostics []ValidationDiagnostic    `json:"diagnostics"`
	Marks       map[string]ValidationMark `json:"marks"`
	Truncated   bool                      `json:"truncated"`
}
type offlineSchemaLoader struct{}

func (offlineSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema references are disabled: %s", url)
}

func CompileSchema(source []byte) (*CompiledSchema, error) {
	if len(source) > MaxSchemaBytes {
		return nil, errors.New("schema exceeds 1 MiB")
	}
	d, err := Parse("json", source)
	if err != nil {
		return nil, err
	}
	if d.Root.Kind != "object" && d.Root.Kind != "boolean" {
		return nil, errors.New("schema must be a JSON object or boolean")
	}
	if len(d.Nodes) > 10000 {
		return nil, errors.New("schema exceeds 10000 nodes")
	}
	for _, n := range d.Nodes {
		depth := 0
		for parent := n.parent; parent != nil; parent = parent.parent {
			depth++
		}
		if depth > 64 {
			return nil, errors.New("schema exceeds depth 64")
		}
		if n.Kind == "number" {
			if _, err := schemaNumber(n); err != nil {
				return nil, err
			}
		}
	}
	value, err := js.UnmarshalJSON(strings.NewReader(string(source)))
	if err != nil {
		return nil, err
	}
	compiler := js.NewCompiler()
	compiler.DefaultDraft(js.Draft2020)
	compiler.UseLoader(offlineSchemaLoader{})
	const resource = "https://datatree.invalid/attached-schema.json"
	if err := compiler.AddResource(resource, value); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile(resource)
	if err != nil {
		return nil, err
	}
	result := &CompiledSchema{schema: schema, locations: map[string]*js.Schema{}}
	var index func(*js.Schema)
	index = func(s *js.Schema) {
		if s == nil {
			return
		}
		if _, ok := result.locations[s.Location]; ok {
			return
		}
		result.locations[s.Location] = s
		children := []*js.Schema{s.Ref, s.RecursiveRef, s.Not, s.If, s.Then, s.Else, s.PropertyNames, s.UnevaluatedProperties, s.UnevaluatedItems, s.Items2020, s.Contains, s.ContentSchema}
		if s.DynamicRef != nil {
			children = append(children, s.DynamicRef.Ref)
		}
		children = append(children, s.AllOf...)
		children = append(children, s.AnyOf...)
		children = append(children, s.OneOf...)
		children = append(children, s.PrefixItems...)
		for _, c := range s.Properties {
			children = append(children, c)
		}
		for _, c := range s.PatternProperties {
			children = append(children, c)
		}
		for _, c := range s.DependentSchemas {
			children = append(children, c)
		}
		for _, v := range []any{s.AdditionalProperties, s.Items, s.AdditionalItems} {
			switch c := v.(type) {
			case *js.Schema:
				children = append(children, c)
			case []*js.Schema:
				children = append(children, c...)
			}
		}
		for _, v := range s.Dependencies {
			if c, ok := v.(*js.Schema); ok {
				children = append(children, c)
			}
		}
		for _, c := range children {
			index(c)
		}
	}
	index(schema)
	return result, nil
}

func ValidateSchema(d *Document, s *CompiledSchema) (ValidationResult, error) {
	result := ValidationResult{Complete: true, Diagnostics: []ValidationDiagnostic{}, Marks: map[string]ValidationMark{}}
	if d == nil || s == nil {
		return result, errors.New("open a document and attach a schema first")
	}
	if d.Format != "json" && d.Format != "yaml" && d.Format != "csv" && d.Format != "jsonl" {
		return result, errors.New("schema validation supports JSON, JSONL, YAML and CSV")
	}
	rawNodes := map[*yaml.Node]*Node{}
	for _, n := range d.Nodes {
		rawNodes[n.raw] = n
	}
	projection := &schemaProjection{rawNodes: rawNodes, visiting: map[*yaml.Node]bool{}, merges: newMergeResolver()}
	roots := []*Node{d.Root}
	if d.Root.Kind == "stream" || d.Format == "jsonl" {
		roots = d.Root.Children
	}
	printer := message.NewPrinter(language.English)
	remainingWork := schemaWorkLimit
	for index, root := range roots {
		instance := schemaInstance{nodes: map[string]*Node{}, root: root, index: index}
		value, err := projection.project(root.raw, root, nil, nil, 0, &instance)
		if err != nil {
			result.Complete = false
			target := projection.target
			if target == nil {
				target = root
			}
			markValidationNode(&result, target)
			if len(result.Diagnostics) < 200 {
				result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Target: target.ID, DocumentIndex: index, Location: target.Location, Keyword: "projection", Message: "Cannot validate: " + err.Error()})
			} else {
				result.Truncated = true
			}
			continue
		}
		instance.value = value
		if err := s.checkWork(value, &remainingWork); err != nil {
			result.Complete = false
			markValidationNode(&result, root)
			if len(result.Diagnostics) < 200 {
				result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Target: root.ID, DocumentIndex: index, Location: root.Location, Keyword: "workLimit", Message: err.Error()})
			} else {
				result.Truncated = true
			}
			continue
		}
		err = s.schema.Validate(value)
		if err == nil {
			continue
		}
		var validation *js.ValidationError
		if !errors.As(err, &validation) {
			return result, err
		}
		var collect func(*js.ValidationError, *string)
		collect = func(e *js.ValidationError, property *string) {
			if k, ok := e.ErrorKind.(*kind.PropertyNames); ok {
				property = &k.Property
			}
			if _, ok := e.ErrorKind.(*kind.RefCycle); ok {
				result.Complete = false
			}
			if len(e.Causes) > 0 {
				for _, c := range e.Causes {
					collect(c, property)
				}
				return
			}
			parts := e.InstanceLocation
			paths := [][]string{parts}
			if k, ok := e.ErrorKind.(*kind.AdditionalProperties); ok {
				paths = nil
				for _, name := range k.Properties {
					paths = append(paths, append(append([]string(nil), parts...), name))
				}
			}
			if property != nil {
				paths = [][]string{append(append([]string(nil), parts...), *property)}
			}
			keyword := "schema"
			if path := e.ErrorKind.KeywordPath(); len(path) > 0 {
				keyword = path[0]
			}
			for _, parts := range paths {
				path := pointer(parts)
				target := instance.nodes[path]
				for target == nil && len(parts) > 0 {
					parts = parts[:len(parts)-1]
					target = instance.nodes[pointer(parts)]
				}
				if target == nil {
					target = root
				}
				markValidationNode(&result, target)
				if len(result.Diagnostics) >= 200 {
					result.Truncated = true
					continue
				}
				diagnostic := ValidationDiagnostic{Target: target.ID, InstancePath: boundedSchemaText(path, 1024), DocumentIndex: index, Location: target.Location, Keyword: keyword, SchemaPath: boundedSchemaText(e.SchemaURL, 1024), Message: boundedSchemaText(e.ErrorKind.LocalizedString(printer), 4096)}
				var values []any
				switch k := e.ErrorKind.(type) {
				case *kind.Enum:
					values = k.Want
				case *kind.Const:
					values = []any{k.Want}
				default:
					schema := s.locations[e.SchemaURL]
					seen := map[*js.Schema]bool{}
					for schema != nil && !seen[schema] {
						seen[schema] = true
						if schema.Enum != nil {
							values = append(values, schema.Enum.Values...)
						}
						if schema.Const != nil {
							values = append(values, *schema.Const)
						}
						schema = schema.Ref
					}
				}
				for _, v := range values {
					if len(diagnostic.Suggestions) == 50 {
						diagnostic.SuggestionsTruncated = true
						break
					}
					b, _ := json.Marshal(v)
					text := string(b)
					if len(text) > 1024 {
						diagnostic.SuggestionsTruncated = true
						text = boundedSchemaText(text, 1024)
					}
					diagnostic.Suggestions = append(diagnostic.Suggestions, text)
				}
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
		}
		collect(validation, nil)
	}
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		a, b := result.Diagnostics[i], result.Diagnostics[j]
		if a.DocumentIndex != b.DocumentIndex {
			return a.DocumentIndex < b.DocumentIndex
		}
		ai, _ := strconv.Atoi(a.Target)
		bi, _ := strconv.Atoi(b.Target)
		if ai != bi {
			return ai < bi
		}
		if a.InstancePath != b.InstancePath {
			return a.InstancePath < b.InstancePath
		}
		if a.SchemaPath != b.SchemaPath {
			return a.SchemaPath < b.SchemaPath
		}
		return a.Keyword < b.Keyword
	})
	result.Valid = result.Complete && len(result.Diagnostics) == 0
	for _, e := range result.Diagnostics {
		n := d.Nodes[e.Target]
		markValidationNode(&result, n)
	}
	if len(result.Diagnostics) > 200 {
		result.Diagnostics = result.Diagnostics[:200]
		result.Truncated = true
	}
	return result, nil
}

func markValidationNode(result *ValidationResult, n *Node) {
	mark := result.Marks[n.ID]
	mark.Direct = true
	result.Marks[n.ID] = mark
	for parent := n.parent; parent != nil; parent = parent.parent {
		mark := result.Marks[parent.ID]
		if mark.Contains {
			break
		}
		mark.Contains = true
		result.Marks[parent.ID] = mark
	}
}

func boundedSchemaText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && (text[limit]&0xc0) == 0x80 {
		limit--
	}
	return text[:limit] + "…"
}
