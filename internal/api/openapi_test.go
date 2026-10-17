package api

// The OpenAPI document is a contract, not documentation: this test fails when a
// DTO field or a registered route stops being described. It deliberately reads
// api/openapi.json and internal/api/server.go rather than a copy of their
// contents.
//
// Scope of the schema check: the spec's `properties` must be EXACTLY the Go
// struct's JSON tag names. Required-ness and `omitempty` are not compared: a
// property may be optional in Go and still be listed, because the contract is
// about which fields exist, not about when they are emitted.

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/sources"
	"fronius-supervisor/internal/storage"
)

const openAPISpecPath = "../../api/openapi.json"

// openAPISchemaTypes maps every component schema to the Go type it mirrors.
// The map is authoritative in both directions: a schema without an entry here,
// or a DTO without a schema, fails the test.
var openAPISchemaTypes = map[string]reflect.Type{
	"Meta":                 reflect.TypeOf(meta{}),
	"ApiError":             reflect.TypeOf(apiError{}),
	"ErrorEnvelope":        reflect.TypeOf(errorEnvelope{}),
	"HealthDTO":            reflect.TypeOf(healthDTO{}),
	"DBHealthDTO":          reflect.TypeOf(dbHealthDTO{}),
	"CollectorHealthDTO":   reflect.TypeOf(collectorHealthDTO{}),
	"SystemDTO":            reflect.TypeOf(systemDTO{}),
	"DeviceDTO":            reflect.TypeOf(deviceDTO{}),
	"DetectionDTO":         reflect.TypeOf(detectionDTO{}),
	"DetectionResourceDTO": reflect.TypeOf(detectionResourceDTO{}),
	"ClockResourceDTO":     reflect.TypeOf(clockResourceDTO{}),
	"ServiceResourceDTO":   reflect.TypeOf(serviceResourceDTO{}),
	"ConnectivityDTO":      reflect.TypeOf(connectivityDTO{}),
	"ConnectivityProbeDTO": reflect.TypeOf(connectivityProbeDTO{}),
	"SampleDTO":            reflect.TypeOf(sampleDTO{}),
	"InverterDTO":          reflect.TypeOf(inverterDTO{}),
	"DerivedEnergyDTO":     reflect.TypeOf(derivedEnergyDTO{}),
	"BucketDTO":            reflect.TypeOf(bucketDTO{}),
	"DailyEnergyDTO":       reflect.TypeOf(dailyEnergyDTO{}),
	"EventDTO":             reflect.TypeOf(eventDTO{}),
	"CapabilityDTO":        reflect.TypeOf(capabilityDTO{}),
	"SourcesDTO":           reflect.TypeOf(sourcesDTO{}),
	"SurveyDTO":            reflect.TypeOf(surveyDTO{}),
	"SourceDTO":            reflect.TypeOf(sourceDTO{}),
	"ProbeDTO":             reflect.TypeOf(probeDTO{}),
	"ProbeField":           reflect.TypeOf(storage.ProbeField{}),
	"Source":               reflect.TypeOf(sources.Source{}),
	"SourceSummary":        reflect.TypeOf(sources.Summary{}),
	"SourceField":          reflect.TypeOf(sources.Field{}),
	"SourceProof":          reflect.TypeOf(sources.Proof{}),
	"DetailDTO":            reflect.TypeOf(detailDTO{}),
	"DetailFamilyDTO":      reflect.TypeOf(detailFamilyDTO{}),
	"DetailMeasurementDTO": reflect.TypeOf(detailMeasurementDTO{}),
	"InverterDetailDTO":    reflect.TypeOf(inverterDetailDTO{}),
	"InverterStateDetail":  reflect.TypeOf(model.InverterStateDetail{}),
	"InverterEnergies":     reflect.TypeOf(model.InverterEnergies{}),
}

// freeFormProperties lists the properties whose value shape is NOT modelled,
// with the reason. Every entry is checked: it must still exist in the spec, it
// must carry a description, and it must not declare a type. Every other
// property must be typed (type, $ref or a composition of them), so a forgotten
// free-form shape fails instead of hiding.
var freeFormProperties = map[string]string{
	"DetailFamilyDTO.data": "detail family payloads are passed through verbatim; their shape is defined by the device family and differs per family",
}

type openAPIDoc struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
	Paths map[string]struct {
		Get *struct {
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"get"`
	} `json:"paths"`
	Components struct {
		Schemas map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

func loadOpenAPIDoc(t *testing.T) openAPIDoc {
	t.Helper()
	raw, err := os.ReadFile(openAPISpecPath)
	if err != nil {
		t.Fatalf("reading %s: %v", openAPISpecPath, err)
	}
	var doc openAPIDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing %s: %v", openAPISpecPath, err)
	}
	return doc
}

// jsonFieldNames returns the JSON tag names of a struct type, recursing into
// embedded structs because their fields are promoted into the JSON object.
// Fields tagged "-" and unexported fields are not part of the contract.
func jsonFieldNames(t reflect.Type) []string {
	var names []string
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			names = append(names, jsonFieldNames(f.Type)...)
			continue
		}
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// symmetricDifference returns the values of want missing from got, then the
// values of got missing from want.
func symmetricDifference(want, got []string) (missing, extra []string) {
	in := func(list []string, v string) bool {
		for _, item := range list {
			if item == v {
				return true
			}
		}
		return false
	}
	for _, v := range want {
		if !in(got, v) {
			missing = append(missing, v)
		}
	}
	for _, v := range got {
		if !in(want, v) {
			extra = append(extra, v)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func TestOpenAPIDocumentIs31(t *testing.T) {
	doc := loadOpenAPIDoc(t)

	if doc.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q, want 3.1.0", doc.OpenAPI)
	}
	if doc.Info.Version != "1.0.0" {
		t.Errorf("info.version = %q, want 1.0.0", doc.Info.Version)
	}
	if doc.Info.Title == "" {
		t.Error("info.title is empty")
	}
	if len(doc.Servers) != 1 || doc.Servers[0].URL != "http://127.0.0.1:8080" {
		t.Errorf("servers = %+v, want the single http://127.0.0.1:8080", doc.Servers)
	}
	if len(doc.Paths) == 0 {
		t.Error("paths is empty")
	}
	for path, item := range doc.Paths {
		if item.Get == nil {
			t.Errorf("path %s has no GET operation", path)
			continue
		}
		if len(item.Get.Responses) == 0 {
			t.Errorf("path %s declares no response", path)
		}
	}
}

func TestOpenAPISchemasMatchDTOs(t *testing.T) {
	doc := loadOpenAPIDoc(t)

	specNames := sortedKeys(doc.Components.Schemas)
	tableNames := sortedKeys(openAPISchemaTypes)
	missing, extra := symmetricDifference(tableNames, specNames)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("components.schemas and openAPISchemaTypes diverge: schema missing from the spec %v; spec schema with no Go type in the test table %v", missing, extra)
	}

	for _, schemaName := range specNames {
		goType, ok := openAPISchemaTypes[schemaName]
		if !ok {
			continue // already reported by the set comparison above
		}
		specProps := sortedKeys(doc.Components.Schemas[schemaName].Properties)
		goProps := jsonFieldNames(goType)
		missing, extra := symmetricDifference(goProps, specProps)
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("schema %s (%s): properties diverge: missing from the spec %v; present in the spec but not in the Go type %v",
				schemaName, goType, missing, extra)
		}
	}
}

func TestOpenAPIPathsMatchRegisteredRoutes(t *testing.T) {
	doc := loadOpenAPIDoc(t)

	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("reading server.go: %v", err)
	}
	// register(path, handler): capture the path, not the closing parenthesis.
	re := regexp.MustCompile(`\bregister\("([^"]+)"`)
	var registered []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		registered = append(registered, m[1])
	}
	if len(registered) == 0 {
		t.Fatal("no register(\"…\") call parsed out of server.go: the route parser is broken")
	}

	specPaths := sortedKeys(doc.Paths)
	missing, extra := symmetricDifference(registered, specPaths)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("paths diverge from server.go: registered but missing from the spec %v; documented but not registered %v",
			missing, extra)
	}
}

func TestOpenAPIDocumentedPathsAnswer(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	_, srv, _ := newTestServer(t, nil)

	paths := sortedKeys(doc.Paths)
	checked := make([]string, 0, len(paths))
	for _, path := range paths {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Errorf("GET %s: %v", path, err)
			continue
		}
		var body struct {
			Error *apiError `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			code := ""
			if body.Error != nil {
				code = body.Error.Code
			}
			t.Errorf("path %s is documented but answers 404 (error.code=%q): the route is not registered",
				path, code)
			continue
		}
		checked = append(checked, path+"="+http.StatusText(resp.StatusCode))
	}
	t.Logf("hit %d documented paths through the test server: %s", len(checked), strings.Join(checked, ", "))
}

func TestOpenAPIPropertiesAreTypedOrDeliberatelyFreeForm(t *testing.T) {
	doc := loadOpenAPIDoc(t)

	found := make(map[string]bool, len(freeFormProperties))
	for schemaName, schema := range doc.Components.Schemas {
		for prop, raw := range schema.Properties {
			var p struct {
				Ref                  string            `json:"$ref"`
				Type                 json.RawMessage   `json:"type"`
				OneOf                []json.RawMessage `json:"oneOf"`
				AllOf                []json.RawMessage `json:"allOf"`
				AnyOf                []json.RawMessage `json:"anyOf"`
				AdditionalProperties json.RawMessage   `json:"additionalProperties"`
				Description          string            `json:"description"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Errorf("schema %s property %s: %v", schemaName, prop, err)
				continue
			}
			key := schemaName + "." + prop
			freeForm, exempt := freeFormProperties[key]
			if exempt {
				found[key] = true
				if p.Ref != "" || len(p.Type) > 0 || len(p.OneOf) > 0 || len(p.AllOf) > 0 || len(p.AnyOf) > 0 {
					t.Errorf("property %s is listed as free-form but declares a type: %s", key, freeForm)
				}
				if string(p.AdditionalProperties) != "true" {
					t.Errorf("free-form property %s must declare additionalProperties: true, got %s", key, p.AdditionalProperties)
				}
				if p.Description == "" {
					t.Errorf("free-form property %s has no description; the exemption must be explained in the spec", key)
				}
				t.Logf("free-form property %s exempted: %s", key, freeForm)
				continue
			}
			typed := p.Ref != "" || len(p.Type) > 0 || len(p.OneOf) > 0 || len(p.AllOf) > 0 || len(p.AnyOf) > 0
			if len(p.AdditionalProperties) > 0 && string(p.AdditionalProperties) != "false" {
				typed = true
			}
			if !typed {
				t.Errorf("property %s has no type, $ref or composition and is not in freeFormProperties: an unmodelled shape must be an explicit exemption", key)
			}
		}
	}

	for key, reason := range freeFormProperties {
		if !found[key] {
			t.Errorf("freeFormProperties names %q (%s) but no such property exists in the spec", key, reason)
		}
	}
}
