package allisall

// These tests hold the hand-written wire types to the OpenAPI spec, until
// generated types replace them. The module must build alone and import
// nothing, so the spec is read from its place in the monorepo, beside
// this module, with a small reader for the few shapes needed rather than
// a YAML module. Outside the monorepo, such as in the public mirror, the
// spec is absent and the tests skip.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const specPath = "../openapi/openapi.yaml"

// readSpec returns the spec's lines, or skips outside the monorepo.
func readSpec(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(specPath)
	if os.IsNotExist(err) {
		if _, werr := os.Stat(filepath.Join("..", "..", "pnpm-workspace.yaml")); werr == nil {
			t.Fatalf("in the monorepo, but %s is missing", specPath)
		}
		t.Skipf("%s is absent outside the monorepo", specPath)
	}
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	return strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// schemaBlock returns the lines of components.schemas.<name>, without its
// own line.
func schemaBlock(lines []string, name string) []string {
	inSchemas := false
	for i, l := range lines {
		if l == "  schemas:" {
			inSchemas = true
			continue
		}
		if !inSchemas || l != "    "+name+":" {
			continue
		}
		end := i + 1
		for end < len(lines) && (strings.TrimSpace(lines[end]) == "" || indentOf(lines[end]) > 4) {
			end++
		}
		return lines[i+1 : end]
	}
	return nil
}

var (
	constLine    = regexp.MustCompile(`^        - const: ([a-z_]+)$`)
	enumItemLine = regexp.MustCompile(`^        - ([a-z_]+)$`)
	propertyLine = regexp.MustCompile(`^        ([a-z_]+):$`)
)

// specConsts returns the const values of a oneOf catalogue.
func specConsts(lines []string, name string) []string {
	var out []string
	for _, l := range schemaBlock(lines, name) {
		if m := constLine.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// specEnum returns the values of a string schema's enum.
func specEnum(lines []string, name string) []string {
	var out []string
	in := false
	for _, l := range schemaBlock(lines, name) {
		switch {
		case l == "      enum:":
			in = true
		case in && enumItemLine.MatchString(l):
			out = append(out, enumItemLine.FindStringSubmatch(l)[1])
		case in && indentOf(l) <= 6:
			in = false
		}
	}
	return out
}

// specProperties returns an object schema's property names.
func specProperties(lines []string, name string) []string {
	var out []string
	in := false
	for _, l := range schemaBlock(lines, name) {
		switch {
		case l == "      properties:":
			in = true
		case in && propertyLine.MatchString(l):
			out = append(out, propertyLine.FindStringSubmatch(l)[1])
		case in && strings.TrimSpace(l) != "" && indentOf(l) <= 6:
			in = false
		}
	}
	return out
}

// sdkConsts returns the values of the constants of the named type
// declared in this package's non-test files.
func sdkConsts(t *testing.T, typeName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec) // a const declaration holds only ValueSpecs
				ident, ok := vs.Type.(*ast.Ident)
				if !ok || ident.Name != typeName {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("a %s constant is not a string literal", typeName)
					}
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// jsonMembers returns the JSON member names of a struct type.
func jsonMembers(typ reflect.Type) []string {
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		out = append(out, name)
	}
	return out
}

// sameSet reports the members only spec has and only sdk has.
func sameSet(spec, sdk []string) (onlySpec, onlySDK []string) {
	for _, s := range spec {
		if !slices.Contains(sdk, s) {
			onlySpec = append(onlySpec, s)
		}
	}
	for _, s := range sdk {
		if !slices.Contains(spec, s) {
			onlySDK = append(onlySDK, s)
		}
	}
	return onlySpec, onlySDK
}

type specCheck struct {
	name string
	spec func(lines []string) []string
	sdk  func(t *testing.T) []string
}

var specChecks = []specCheck{
	{"ErrorCode", func(l []string) []string { return specConsts(l, "ErrorCode") }, func(t *testing.T) []string { return sdkConsts(t, "ErrorCode") }},
	{"FieldErrorCode", func(l []string) []string { return specConsts(l, "FieldErrorCode") }, func(t *testing.T) []string { return sdkConsts(t, "FieldErrorCode") }},
	{"EventCategory", func(l []string) []string { return specEnum(l, "EventCategory") }, func(t *testing.T) []string { return sdkConsts(t, "EventCategory") }},
	{"EventInput", func(l []string) []string { return specProperties(l, "EventInput") }, func(*testing.T) []string { return jsonMembers(reflect.TypeFor[EventInput]()) }},
	{"IngestRowError", func(l []string) []string { return specProperties(l, "IngestRowError") }, func(*testing.T) []string { return jsonMembers(reflect.TypeFor[IngestRowError]()) }},
	{"IngestResult", func(l []string) []string { return specProperties(l, "IngestResult") }, func(*testing.T) []string { return jsonMembers(reflect.TypeFor[ingestResult]()) }},
	{"Problem", func(l []string) []string { return specProperties(l, "Problem") }, func(*testing.T) []string { return jsonMembers(reflect.TypeFor[Problem]()) }},
}

func TestHandWrittenTypesMatchTheSpec(t *testing.T) {
	lines := readSpec(t)
	for _, c := range specChecks {
		t.Run(c.name, func(t *testing.T) {
			spec, sdk := c.spec(lines), c.sdk(t)
			if len(spec) == 0 {
				t.Fatalf("read nothing for %s from the spec: the reader no longer fits its layout", c.name)
			}
			if onlySpec, onlySDK := sameSet(spec, sdk); len(onlySpec) > 0 || len(onlySDK) > 0 {
				t.Errorf("%s: only in the spec %v; only in the SDK %v", c.name, onlySpec, onlySDK)
			}
			if dup := len(sdk) - len(slices.Compact(slices.Sorted(slices.Values(sdk)))); dup != 0 {
				t.Errorf("%s: the SDK lists a value twice", c.name)
			}
		})
	}
}

// TestSpecCheckCanFail removes a value from the spec's text and checks
// that the comparison then reports it, so the test above cannot pass on a
// reader that silently reads nothing or everything.
func TestSpecCheckCanFail(t *testing.T) {
	lines := readSpec(t)
	mutations := []struct {
		check, drop, missing string
	}{
		{"ErrorCode", "        - const: rate_limited", "rate_limited"},
		{"FieldErrorCode", "        - const: too_large", "too_large"},
		{"EventCategory", "        - outcome", "outcome"},
		{"EventInput", "        tokens_out:", "tokens_out"},
	}
	for _, m := range mutations {
		t.Run(m.check, func(t *testing.T) {
			var c specCheck
			for _, sc := range specChecks {
				if sc.name == m.check {
					c = sc
				}
			}
			block := schemaBlock(lines, m.check)
			if !slices.Contains(block, m.drop) {
				t.Fatalf("the %s block has no line %q", m.check, m.drop)
			}
			mutated := slices.DeleteFunc(slices.Clone(lines), func(l string) bool {
				return l == m.drop && slices.Contains(block, l)
			})
			_, onlySDK := sameSet(c.spec(mutated), c.sdk(t))
			if !slices.Equal(onlySDK, []string{m.missing}) {
				t.Errorf("after removing %q, only in the SDK = %v, want [%s]", m.drop, onlySDK, m.missing)
			}
		})
	}
}
