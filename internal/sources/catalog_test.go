// Tests of the source catalogue.
//
// The catalogue is only worth trusting if it cannot drift from the registers it
// summarizes. These tests bind it to the four things it claims to describe: the
// code allowlist (the executable authorization), the capability ids of
// internal/model, the fixtures on disk, and the two registers that enumerate the
// paths (`docs/SOURCES.md`, `docs/ENDPOINTS.md`).
package sources

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/model"
)

// repoFile reads a file from the repository root (the tests run in this package
// directory).
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(body)
}

// capabilityIDs is the set of capability ids the model can publish.
func capabilityIDs() map[string]bool {
	out := make(map[string]bool)
	for _, c := range model.Capabilities(model.Detection{}) {
		out[c.ID] = true
	}
	return out
}

func TestIdentifiersAreUniqueAndSorted(t *testing.T) {
	list := Catalog()
	if len(list) == 0 {
		t.Fatal("the catalogue is empty")
	}
	seen := make(map[string]bool, len(list))
	for _, s := range list {
		if s.ID == "" {
			t.Errorf("a source has no identifier: %+v", s)
		}
		if seen[s.ID] {
			t.Errorf("duplicate source identifier %q", s.ID)
		}
		seen[s.ID] = true
		if s.Path == "" {
			t.Errorf("%s: no path", s.ID)
		}
		if s.Method != "GET" {
			// The whole project is read-only; a catalogue entry that is not a GET
			// would be a mutation waiting for a caller.
			t.Errorf("%s: method %q, want GET", s.ID, s.Method)
		}
		if s.Provides == "" {
			t.Errorf("%s: nothing says what it provides", s.ID)
		}
		if len(s.Proofs) == 0 {
			t.Errorf("%s: no provenance", s.ID)
		}
		for _, p := range s.Proofs {
			if p.Tag == "" || p.Detail == "" {
				t.Errorf("%s: incomplete proof %+v", s.ID, p)
			}
		}
	}

	ids := IDs()
	if !sort.StringsAreSorted(ids) {
		t.Errorf("IDs() is not sorted: %v", ids)
	}
	if len(ids) != len(list) {
		t.Errorf("IDs() returned %d identifiers for %d sources", len(ids), len(list))
	}
}

func TestFieldsCarryTheirReason(t *testing.T) {
	valid := map[FieldStatus]bool{
		FieldDisplayed: true, FieldStored: true, FieldValidated: true,
		FieldIgnored: true, FieldUnverified: true,
	}
	for _, s := range Catalog() {
		for _, f := range s.Fields {
			if f.Path == "" || f.Type == "" {
				t.Errorf("%s: incomplete field %+v", s.ID, f)
			}
			if !valid[f.Status] {
				t.Errorf("%s: %s has status %q", s.ID, f.Path, f.Status)
			}
			// An ignored or unverified field is a judgement call: without the
			// reason it becomes an unexplained gap.
			if (f.Status == FieldIgnored || f.Status == FieldUnverified) && f.Why == "" {
				t.Errorf("%s: %s is %s with no reason", s.ID, f.Path, f.Status)
			}
		}
	}
}

func TestCapabilitiesExistOrAreFuture(t *testing.T) {
	known := capabilityIDs()
	// A field may name a candidate capability that the catalogue introduces
	// elsewhere: `inverter.state` is the unlock of the accumulation variant and
	// is carried by the same DeviceStatus object of the common variant.
	candidates := map[string]bool{}
	for _, s := range Catalog() {
		if s.WouldUnlock != "" {
			candidates[s.WouldUnlock] = true
		}
	}
	for _, s := range Catalog() {
		if s.Capability != "" && !known[s.Capability] {
			t.Errorf("%s: feeds unknown capability %q", s.ID, s.Capability)
		}
		// A "would unlock" id names a capability that does not exist yet: it is
		// what wiring the source would create.
		if s.WouldUnlock != "" && known[s.WouldUnlock] {
			t.Errorf("%s: %q already exists, it is not something to unlock", s.ID, s.WouldUnlock)
		}
		for _, f := range s.Fields {
			if f.Capability == "" {
				continue
			}
			if known[f.Capability] || candidates[f.Capability] {
				continue
			}
			t.Errorf("%s: %s names capability %q, which neither exists nor is introduced by the catalogue",
				s.ID, f.Path, f.Capability)
		}
	}
}

func TestCalledSourcesAreExactlyTheCodeAllowlist(t *testing.T) {
	fromCatalogue := map[string]bool{}
	for _, s := range Catalog() {
		if s.Called {
			fromCatalogue[s.Path] = true
		}
	}

	// The internal side: the allowlist is the only executable authorization of
	// the project, so the two sets must be equal — in both directions.
	fromAllowlist := map[string]bool{}
	for _, ep := range internalapi.Allowlist() {
		fromAllowlist[ep.Path] = true
	}
	for path := range fromAllowlist {
		if !fromCatalogue[path] {
			t.Errorf("%s is in the code allowlist but not marked as called in the catalogue", path)
		}
	}
	for path := range fromCatalogue {
		if strings.HasPrefix(path, "/solar_api/") {
			continue
		}
		if !fromAllowlist[path] {
			t.Errorf("%s is marked as called but is not in the code allowlist", path)
		}
	}
	if !fromCatalogue[solarapi.PowerFlowPath] {
		t.Errorf("%s is called by the collector but not marked as called", solarapi.PowerFlowPath)
	}
	if len(Called()) != len(fromCatalogue) {
		t.Errorf("Called() returned %d sources, %d paths are marked as called", len(Called()), len(fromCatalogue))
	}
}

func TestCapturedSourcesHaveTheirFixture(t *testing.T) {
	for _, s := range Catalog() {
		if s.Fixture == "" {
			continue
		}
		path := filepath.Join("..", "..", filepath.FromSlash(s.Fixture))
		if st, err := os.Stat(path); err != nil {
			t.Errorf("%s: fixture %s: %v", s.ID, s.Fixture, err)
		} else if st.Size() == 0 {
			t.Errorf("%s: fixture %s is empty", s.ID, s.Fixture)
		}
	}
}

// TestSolarAPIPathsAgreeWithTheRegister keeps the candidate table of
// docs/SOURCES.md and the catalogue in step, in both directions: a path added to
// one without the other fails here.
func TestSolarAPIPathsAgreeWithTheRegister(t *testing.T) {
	doc := repoFile(t, "docs/SOURCES.md")

	// The catalogue must not claim a Solar API path the register never mentions.
	for _, s := range Catalog() {
		if s.Transport != SolarAPI {
			continue
		}
		if !strings.Contains(doc, s.Path) {
			t.Errorf("%s: %s is in the catalogue but absent from docs/SOURCES.md", s.ID, s.Path)
		}
	}

	// Every path of the candidate table must exist in the catalogue.
	section := sectionOf(t, doc, "## Other Solar API endpoints")
	inCatalogue := map[string]bool{}
	for _, s := range Catalog() {
		inCatalogue[s.Path] = true
	}
	for _, path := range backtickedPaths(section) {
		if !inCatalogue[path] {
			t.Errorf("%s is listed as a candidate in docs/SOURCES.md but missing from the catalogue", path)
		}
	}
}

// TestInternalPathsAgreeWithTheRegister does the same for the internal API and
// docs/ENDPOINTS.md.
func TestInternalPathsAgreeWithTheRegister(t *testing.T) {
	doc := repoFile(t, "docs/ENDPOINTS.md")
	for _, s := range Catalog() {
		if s.Transport != InternalAPI {
			continue
		}
		if !strings.Contains(doc, s.Path) {
			t.Errorf("%s: %s is in the catalogue but absent from docs/ENDPOINTS.md", s.ID, s.Path)
		}
	}
}

// TestSummaryCountsTheCatalogue checks the counters the interface displays.
func TestSummaryCountsTheCatalogue(t *testing.T) {
	list := Catalog()
	got := Summarize(list)

	if got.Sources != len(list) {
		t.Errorf("sources = %d, want %d", got.Sources, len(list))
	}
	if got.Called+got.Captured+got.Candidates+got.Archived != got.Sources {
		t.Errorf("states do not add up: %+v", got)
	}
	fields, displayed, stored, validated, ignored, unverified := 0, 0, 0, 0, 0, 0
	for _, s := range list {
		for _, f := range s.Fields {
			fields++
			switch f.Status {
			case FieldDisplayed:
				displayed++
			case FieldStored:
				stored++
			case FieldValidated:
				validated++
			case FieldIgnored:
				ignored++
			case FieldUnverified:
				unverified++
			}
		}
	}
	if got.Fields != fields || got.FieldsDisplayed != displayed || got.FieldsStored != stored ||
		got.FieldsValidated != validated || got.FieldsIgnored != ignored || got.FieldsUnverified != unverified {
		t.Errorf("field counters disagree: got %+v, counted %d/%d/%d/%d/%d/%d",
			got, fields, displayed, stored, validated, ignored, unverified)
	}
	// The catalogue exists to expose the gaps: if nothing is unverified, it has
	// stopped describing a real installation.
	if got.FieldsUnverified == 0 {
		t.Error("no field is unverified: the catalogue looks like a claim of completeness")
	}
}

// TestCatalogIsIndependentOfTheCaller makes sure a caller cannot reorder or
// mutate the package's slices through the returned values.
func TestCatalogIsIndependentOfTheCaller(t *testing.T) {
	first := Catalog()
	original := first[0].Path
	first[0].Path = "mutated"
	first[0].Fields = append(first[0].Fields, Field{Path: "injected"})

	second := Catalog()
	if second[0].Path != original {
		t.Errorf("the catalogue was mutated through a returned slice: %s", second[0].Path)
	}
	for _, f := range second[0].Fields {
		if f.Path == "injected" {
			t.Error("the field slice was mutated through a returned value")
		}
	}
}

// sectionOf returns the text between a heading and the next same-level heading.
func sectionOf(t *testing.T, doc, heading string) string {
	t.Helper()
	start := strings.Index(doc, heading)
	if start < 0 {
		t.Fatalf("docs/SOURCES.md has no %q section", heading)
	}
	rest := doc[start:]
	if end := strings.Index(rest[len(heading):], "\n## "); end >= 0 {
		return rest[:len(heading)+end]
	}
	return rest
}

// backtickedPaths extracts the `/solar_api/...` literals of a markdown fragment.
var pathPattern = regexp.MustCompile("`(/solar_api[^`]*)`")

func backtickedPaths(fragment string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range pathPattern.FindAllStringSubmatch(fragment, -1) {
		// A row may carry a query string or a trailing slash variant: only the
		// path itself identifies the source.
		path := m[1]
		if i := strings.IndexAny(path, "? "); i >= 0 {
			path = path[:i]
		}
		path = strings.TrimSuffix(path, "/")
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}
