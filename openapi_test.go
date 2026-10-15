package memogram

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type schemaRef struct {
	Ref string `yaml:"$ref"`
}

type openAPIMedia struct {
	Schema schemaRef `yaml:"schema"`
}

type openAPIOperation struct {
	ID          string `yaml:"operationId"`
	RequestBody struct {
		Content map[string]openAPIMedia `yaml:"content"`
	} `yaml:"requestBody"`
	Responses map[string]struct {
		Content map[string]openAPIMedia `yaml:"content"`
	} `yaml:"responses"`
}

type openAPISnapshot struct {
	Paths      map[string]map[string]openAPIOperation `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Properties map[string]struct {
				Type   string `yaml:"type"`
				Format string `yaml:"format"`
			} `yaml:"properties"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func TestOpenAPICompatibility(t *testing.T) {
	readYAML := func(path string, target any) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := yaml.Unmarshal(data, target); err != nil {
			t.Fatal(err)
		}
	}
	var config struct {
		OutputOptions struct {
			Operations []string `yaml:"include-operation-ids"`
		} `yaml:"output-options"`
	}
	readYAML("openapi/oapi-codegen.yaml", &config)
	var latest openAPISnapshot
	readYAML("openapi/latest.yaml", &latest)
	type contract struct{ Method, Path, Request, Response string }
	contracts := func(spec openAPISnapshot) map[string]contract {
		result := map[string]contract{}
		for path, methods := range spec.Paths {
			for method, op := range methods {
				if op.ID == "" {
					continue
				}
				result[op.ID] = contract{method, strings.Replace(path, "/api/v1/", "/api/", 1),
					op.RequestBody.Content["application/json"].Schema.Ref,
					op.Responses["200"].Content["application/json"].Schema.Ref}
			}
		}
		return result
	}
	latestContracts := contracts(latest)
	for _, version := range []string{"0-30", "0-31"} {
		t.Run(version, func(t *testing.T) {
			var stable openAPISnapshot
			readYAML("openapi/"+version+".yaml", &stable)
			stableContracts := contracts(stable)
			for _, id := range config.OutputOptions.Operations {
				want, ok := latestContracts[id]
				if !ok {
					t.Fatalf("generated operation %s missing from latest", id)
				}
				if got := stableContracts[id]; got != want {
					t.Errorf("incompatible operation %s: stable=%+v latest=%+v", id, got, want)
				}
			}
			for model, fields := range map[string][]string{
				"Memo":            {"name", "creator", "content", "visibility", "pinned"},
				"Attachment":      {"name", "filename", "type", "content", "memo", "size"},
				"User":            {"name", "username", "displayName"},
				"InstanceProfile": {"version", "instanceUrl"},
			} {
				for _, field := range fields {
					got, exists := stable.Components.Schemas[model].Properties[field]
					want := latest.Components.Schemas[model].Properties[field]
					if !exists || !reflect.DeepEqual(got, want) {
						t.Errorf("incompatible field %s.%s: stable=%+v latest=%+v", model, field, got, want)
					}
				}
			}
		})
	}
}
