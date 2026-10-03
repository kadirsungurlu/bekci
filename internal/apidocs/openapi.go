// Package apidocs OpenAPI belgesini ikiliye gömer (GET /api/openapi.json).
// Kaynak internal/apidocs/openapi.yaml elle yazılır; internal/api içindeki test her
// kayıtlı /api/ rotasının belgede ya da açıkça "iç" listesinde olduğunu denetler.
package apidocs

import _ "embed"

// OpenAPI internal/apidocs/openapi.yaml içeriği (YAML).
//
//go:embed openapi.yaml
var OpenAPI []byte
