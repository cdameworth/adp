package docengine

import (
	"fmt"
	"strings"
)

// DocSpec represents the agent-native documentation specification
type DocSpec struct {
	Title       string            `yaml:"title"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Endpoints   []EndpointSpec    `yaml:"endpoints"`
	Schemas     map[string]Schema `yaml:"schemas"`
}

type EndpointSpec struct {
	Method      string `yaml:"method"`
	Path        string `yaml:"path"`
	Description string `yaml:"description"`
}

type Schema struct {
	Type       string              `yaml:"type"`
	Properties map[string]Property `yaml:"properties"`
}

type Property struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
}

// GenerateMarkdown converts a DocSpec into human-readable Markdown
func GenerateMarkdown(spec DocSpec) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", spec.Title))
	sb.WriteString(fmt.Sprintf("**Version:** %s\n\n", spec.Version))
	sb.WriteString(fmt.Sprintf("%s\n\n", spec.Description))

	sb.WriteString("## Endpoints\n\n")
	for _, ep := range spec.Endpoints {
		sb.WriteString(fmt.Sprintf("### %s %s\n\n", ep.Method, ep.Path))
		sb.WriteString(fmt.Sprintf("%s\n\n", ep.Description))
	}

	sb.WriteString("## Schemas\n\n")
	for name, schema := range spec.Schemas {
		sb.WriteString(fmt.Sprintf("### %s\n\n", name))
		sb.WriteString(fmt.Sprintf("Type: `%s`\n\n", schema.Type))
		if len(schema.Properties) > 0 {
			sb.WriteString("| Property | Type | Description |\n")
			sb.WriteString("|----------|------|-------------|\n")
			for propName, prop := range schema.Properties {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", propName, prop.Type, prop.Description))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
