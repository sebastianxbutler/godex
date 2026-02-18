package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// UpdateAliases reads the config file, updates the aliases map under
// proxy.backends.routing.aliases, and writes it back preserving other content.
func UpdateAliases(path string, aliases map[string]string) error {
	buf, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(buf, &root); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Navigate: root → document → proxy → backends → routing → aliases
	aliasNode := findNode(&root, "proxy", "backends", "routing", "aliases")
	if aliasNode == nil {
		return fmt.Errorf("aliases section not found in config")
	}

	// Rebuild the aliases mapping node
	aliasNode.Content = nil
	// Sort keys for deterministic output
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		aliasNode.Content = append(aliasNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Value: aliases[k]},
		)
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// yaml.Marshal adds a document separator; strip it if original didn't have one
	outStr := string(out)
	if !strings.HasPrefix(string(buf), "---") && strings.HasPrefix(outStr, "---") {
		outStr = strings.TrimPrefix(outStr, "---\n")
	}

	if err := os.WriteFile(path, []byte(outStr), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// findNode navigates a yaml.Node tree by map keys.
func findNode(node *yaml.Node, keys ...string) *yaml.Node {
	if node == nil {
		return nil
	}
	// Unwrap document node
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return findNode(node.Content[0], keys...)
	}
	if len(keys) == 0 {
		return node
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	key := keys[0]
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return findNode(node.Content[i+1], keys[1:]...)
		}
	}
	return nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
