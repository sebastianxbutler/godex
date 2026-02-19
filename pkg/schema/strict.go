package schema

// NormalizeStrictSchemaNode recursively enforces strict JSON-schema object rules:
// - Object nodes are closed (`additionalProperties: false`)
// - Optional object properties are made nullable and added to `required`
//
// This matches strict tool-schema requirements used by provider APIs.
func NormalizeStrictSchemaNode(node any) any {
	switch n := node.(type) {
	case map[string]any:
		normalizeStrictObjectIfPresent(n)
		for _, k := range []string{"anyOf", "oneOf", "allOf"} {
			if raw, ok := n[k].([]any); ok {
				for i := range raw {
					raw[i] = NormalizeStrictSchemaNode(raw[i])
				}
				n[k] = raw
			}
		}
		if raw, ok := n["items"]; ok {
			n["items"] = NormalizeStrictSchemaNode(raw)
		}
		if raw, ok := n["prefixItems"].([]any); ok {
			for i := range raw {
				raw[i] = NormalizeStrictSchemaNode(raw[i])
			}
			n["prefixItems"] = raw
		}
		if raw, ok := n["properties"].(map[string]any); ok {
			for name, prop := range raw {
				raw[name] = NormalizeStrictSchemaNode(prop)
			}
			n["properties"] = raw
		}
		if raw, ok := n["additionalProperties"]; ok {
			n["additionalProperties"] = NormalizeStrictSchemaNode(raw)
		}
		return n
	case []any:
		for i := range n {
			n[i] = NormalizeStrictSchemaNode(n[i])
		}
		return n
	default:
		return node
	}
}

func normalizeStrictObjectIfPresent(schema map[string]any) {
	typ, _ := schema["type"].(string)
	if typ == "" && (schema["properties"] != nil || schema["required"] != nil) {
		schema["type"] = "object"
		typ = "object"
	}
	hasObjectType := typ == "object"
	if !hasObjectType {
		if tarr, ok := schema["type"].([]any); ok {
			for _, v := range tarr {
				if s, ok := v.(string); ok && s == "object" {
					hasObjectType = true
					break
				}
			}
		}
	}
	if !hasObjectType {
		return
	}

	if ap, ok := schema["additionalProperties"]; !ok || ap != false {
		schema["additionalProperties"] = false
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return
	}

	requiredSet := map[string]bool{}
	required := []any{}
	if raw, ok := schema["required"].([]any); ok {
		for _, v := range raw {
			s, ok := v.(string)
			if !ok || s == "" || requiredSet[s] {
				continue
			}
			requiredSet[s] = true
			required = append(required, s)
		}
	}

	for name, prop := range props {
		if requiredSet[name] {
			continue
		}
		props[name] = makeSchemaNullable(prop)
		requiredSet[name] = true
		required = append(required, name)
	}

	schema["properties"] = props
	schema["required"] = required
}

func makeSchemaNullable(prop any) any {
	m, ok := prop.(map[string]any)
	if !ok {
		return map[string]any{
			"anyOf": []any{prop, map[string]any{"type": "null"}},
		}
	}

	if rawType, ok := m["type"]; ok {
		switch t := rawType.(type) {
		case string:
			if t != "null" {
				m["type"] = []any{t, "null"}
			}
			return m
		case []any:
			for _, v := range t {
				if s, ok := v.(string); ok && s == "null" {
					return m
				}
			}
			m["type"] = append(t, "null")
			return m
		}
	}

	if rawAnyOf, ok := m["anyOf"].([]any); ok {
		for _, v := range rawAnyOf {
			if mm, ok := v.(map[string]any); ok {
				if s, _ := mm["type"].(string); s == "null" {
					return m
				}
			}
		}
		m["anyOf"] = append(rawAnyOf, map[string]any{"type": "null"})
		return m
	}

	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return map[string]any{
		"anyOf": []any{out, map[string]any{"type": "null"}},
	}
}
