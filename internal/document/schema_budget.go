package document

import (
	"errors"
	"strconv"

	js "github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaWorkLimit = 100000

// Conservatively count potentially evaluated schema/value pairs before calling
// the third-party evaluator, which otherwise constructs every error in memory.
// Count both conditional branches and every alternative, even if one succeeds.
func (s *CompiledSchema) checkWork(value any, remaining *int) error {
	spend := func(n int) error {
		*remaining -= n
		if *remaining < 0 {
			return errors.New("validation work limit exceeded; narrow the schema or document")
		}
		return nil
	}
	var size func(any) int
	size = func(v any) int {
		n := 1
		switch v := v.(type) {
		case map[string]any:
			for _, c := range v {
				n += size(c)
			}
		case []any:
			for _, c := range v {
				n += size(c)
			}
		case string:
			n += len(v) / 64
		}
		return n
	}
	type pair struct {
		schema *js.Schema
		path   string
	}
	active := map[pair]bool{}
	var walk func(*js.Schema, any, string, int) error
	walk = func(schema *js.Schema, v any, path string, depth int) error {
		if schema == nil {
			return nil
		}
		if err := spend(1); err != nil {
			return err
		}
		if depth > 512 {
			return errors.New("validation work limit exceeded (schema evaluation depth)")
		}
		key := pair{schema, path}
		if active[key] {
			return nil
		}
		active[key] = true
		defer delete(active, key)
		if schema.Bool != nil {
			return nil
		}
		if err := spend(len(schema.Required)); err != nil {
			return err
		}
		if text, ok := v.(string); ok && (schema.Pattern != nil || schema.MinLength != nil || schema.MaxLength != nil || schema.Format != nil) {
			if err := spend(len(text) / 64); err != nil {
				return err
			}
		}
		if schema.Const != nil {
			if err := spend(size(v) + size(*schema.Const)); err != nil {
				return err
			}
		}
		if schema.Enum != nil {
			for _, candidate := range schema.Enum.Values {
				if err := spend(size(v) + size(candidate)); err != nil {
					return err
				}
			}
		}
		children := []*js.Schema{schema.Ref, schema.Not, schema.If, schema.Then, schema.Else, schema.RecursiveRef}
		children = append(children, schema.AllOf...)
		children = append(children, schema.AnyOf...)
		children = append(children, schema.OneOf...)
		if schema.RecursiveRef != nil {
			for _, candidate := range s.locations {
				if candidate.RecursiveAnchor {
					children = append(children, candidate)
				}
			}
		}
		if schema.DynamicRef != nil {
			children = append(children, schema.DynamicRef.Ref)
			if schema.DynamicRef.Anchor != "" {
				for _, candidate := range s.locations {
					if candidate.DynamicAnchor == schema.DynamicRef.Anchor {
						children = append(children, candidate)
					}
				}
			}
		}
		for _, c := range children {
			if err := walk(c, v, path, depth+1); err != nil {
				return err
			}
		}
		switch value := v.(type) {
		case map[string]any:
			for name, child := range value {
				if err := spend(1); err != nil {
					return err
				}
				if err := spend(len(schema.DependentRequired[name])); err != nil {
					return err
				}
				if required, ok := schema.Dependencies[name].([]string); ok {
					if err := spend(len(required)); err != nil {
						return err
					}
				}
				next := path + pointer([]string{name})
				candidates := []*js.Schema{schema.Properties[name], schema.UnevaluatedProperties}
				if c, ok := schema.AdditionalProperties.(*js.Schema); ok {
					candidates = append(candidates, c)
				}
				for pattern, c := range schema.PatternProperties {
					if err := spend(1); err != nil {
						return err
					}
					if pattern.MatchString(name) {
						candidates = append(candidates, c)
					}
				}
				for _, c := range candidates {
					if err := walk(c, child, next, depth+1); err != nil {
						return err
					}
				}
				if err := walk(schema.PropertyNames, name, next+"/~propertyName", depth+1); err != nil {
					return err
				}
				if err := walk(schema.DependentSchemas[name], value, path, depth+1); err != nil {
					return err
				}
				if c, ok := schema.Dependencies[name].(*js.Schema); ok {
					if err := walk(c, value, path, depth+1); err != nil {
						return err
					}
				}
			}
		case []any:
			if schema.UniqueItems {
				if err := spend(size(value) * min(len(value), 20)); err != nil {
					return err
				}
			}
			for index, child := range value {
				if err := spend(1); err != nil {
					return err
				}
				candidates := []*js.Schema{schema.Contains, schema.Items2020, schema.UnevaluatedItems}
				if index < len(schema.PrefixItems) {
					candidates = append(candidates, schema.PrefixItems[index])
				}
				switch items := schema.Items.(type) {
				case *js.Schema:
					candidates = append(candidates, items)
				case []*js.Schema:
					if index < len(items) {
						candidates = append(candidates, items[index])
					} else if c, ok := schema.AdditionalItems.(*js.Schema); ok {
						candidates = append(candidates, c)
					}
				}
				for _, c := range candidates {
					if err := walk(c, child, path+"/"+strconv.Itoa(index), depth+1); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	return walk(s.schema, value, "", 0)
}
