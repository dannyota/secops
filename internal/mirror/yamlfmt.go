package mirror

import (
	"bytes"
	"os"

	"gopkg.in/yaml.v3"
)

// marshalYAML renders v as deterministic YAML (yaml.v3 sorts map keys; 2-space
// indent) for clean, stable git diffs.
func marshalYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// yamlUnmarshal decodes YAML data into v.
func yamlUnmarshal(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

// readYAMLFile reads path and decodes its YAML into v.
func readYAMLFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yamlUnmarshal(data, v)
}

// writeYAML writes v to path as deterministic YAML.
func writeYAML(path string, v any) error {
	b, err := marshalYAML(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
