// Package docs содержит OpenAPI-спецификацию сервиса кошелька.
// Файлы swagger.json и swagger.yaml встраиваются в бинарь при сборке.
package docs

import _ "embed"

//go:embed swagger.json
var specJSON []byte

//go:embed swagger.yaml
var specYAML []byte

// SpecJSON возвращает спецификацию в формате JSON.
func SpecJSON() []byte { return specJSON }

// SpecYAML возвращает спецификацию в формате YAML.
func SpecYAML() []byte { return specYAML }