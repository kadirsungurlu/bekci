// Package docs OpenAPI belgesini ikiliye gömer (GET /api/openapi.json).
// Kaynak docs/openapi.yaml elle yazılır; internal/api içindeki test her
// kayıtlı /api/ rotasının belgede ya da açıkça "iç" listesinde olduğunu denetler.
package docs

import _ "embed"

// OpenAPI docs/openapi.yaml içeriği (YAML).
//
//go:embed openapi.yaml
var OpenAPI []byte
