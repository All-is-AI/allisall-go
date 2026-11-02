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

// constDecl is one constant: its name and its string value.
type constDecl struct{ name, value string }

// sdkConsts returns the values of the constants of the named type
// declared in this package's non-test files.
func sdkConsts(t *testing.T, typeName string) []string {
	t.Helper()
	var out []string
	for _, d := range sdkConstDecls(t, typeName) {
		out = append(out, d.value)
	}
	return out
}

// sdkConstDecls returns the constants of the named type declared in this
// package's non-test files.
func sdkConstDecls(t *testing.T, typeName string) []constDecl {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []constDecl
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
				if len(vs.Values) != len(vs.Names) {
					t.Fatalf("a %s declaration does not give each name one value", typeName)
				}
				for i, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("a %s constant is not a string literal", typeName)
					}
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					out = append(out, constDecl{vs.Names[i].Name, s})
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

// inCondition reports whether block[i] sits under if, then, else or not,
// where a code is a condition on a value rather than a member's definition.
func inCondition(block []string, i int) bool {
	indent := indentOf(block[i])
	for j := i - 1; j >= 0 && indent > 0; j-- {
		if strings.TrimSpace(block[j]) == "" || indentOf(block[j]) >= indent {
			continue
		}
		indent = indentOf(block[j])
		if k := strings.TrimSpace(block[j]); slices.Contains([]string{"if:", "then:", "else:", "not:"}, k) {
			return true
		}
	}
	return false
}

// codeShapes returns, for each member named code in a schema, the trimmed
// lines of its definition. The SDK keeps a code it does not list, which
// is right only while the spec leaves every code open.
func codeShapes(lines []string, name string) [][]string {
	var out [][]string
	block := schemaBlock(lines, name)
	for i, l := range block {
		if strings.TrimSpace(l) != "code:" || inCondition(block, i) {
			continue
		}
		var shape []string
		for _, d := range block[i+1:] {
			if strings.TrimSpace(d) == "" || indentOf(d) <= indentOf(l) {
				break
			}
			shape = append(shape, strings.TrimSpace(d))
		}
		out = append(out, shape)
	}
	return out
}

func TestInCondition(t *testing.T) {
	block := strings.Split(`      if:
        properties:
          code:
            const: validation_failed
      not:
        properties:
          code:
            type: string
      then:
        properties:
          code:
            type: string
      else:
        properties:
          code:
            type: string
      properties:
        code:
          anyOf:`, "\n")
	for i, want := range map[int]bool{2: true, 6: true, 10: true, 14: true, 17: false} {
		if strings.TrimSpace(block[i]) != "code:" {
			t.Fatalf("line %d is %q, not a code", i, block[i])
		}
		if got := inCondition(block, i); got != want {
			t.Errorf("inCondition at line %d = %v, want %v", i, got, want)
		}
	}
}

// openCode is the definition of a code open beyond its catalogue.
func openCode(catalogue string) []string {
	return []string{"anyOf:", "- $ref: '#/components/schemas/" + catalogue + "'", "- type: string"}
}

var codeChecks = []struct {
	schema, catalogue string
	uses              int
}{
	{"Problem", "ErrorCode", 1},
	{"IngestRowError", "FieldErrorCode", 1},
	{"FieldError", "FieldErrorCode", 4}, // one for each kind of locator
}

// codeMismatches reports each schema whose codes are not all open, or
// that holds a different number of codes than expected.
func codeMismatches(lines []string) []string {
	var out []string
	for _, c := range codeChecks {
		shapes := codeShapes(lines, c.schema)
		if len(shapes) != c.uses {
			out = append(out, c.schema+": "+strconv.Itoa(len(shapes))+" codes, want "+strconv.Itoa(c.uses))
		}
		for _, s := range shapes {
			if !slices.Equal(s, openCode(c.catalogue)) {
				out = append(out, c.schema+": a code is "+strings.Join(s, " "))
			}
		}
	}
	return out
}

func TestTheSpecLeavesEveryCodeOpen(t *testing.T) {
	if m := codeMismatches(readSpec(t)); len(m) > 0 {
		t.Errorf("codes the SDK would refuse to keep: %v", m)
	}
}

// TestCodeCheckCanFail closes one code of each schema in the spec's text,
// by blanking the line that opens it, and checks that the comparison then
// reports it.
func TestCodeCheckCanFail(t *testing.T) {
	lines := readSpec(t)
	for _, c := range codeChecks {
		t.Run(c.schema, func(t *testing.T) {
			mutated := slices.Clone(lines)
			// schemaBlock returns a window onto mutated, so a blanked line
			// changes the spec text the check reads.
			block := schemaBlock(mutated, c.schema)
			i := slices.IndexFunc(block, func(l string) bool { return strings.TrimSpace(l) == "- type: string" })
			if i < 0 {
				t.Fatalf("the %s block has no open code to close", c.schema)
			}
			block[i] = ""
			if len(codeMismatches(mutated)) == 0 {
				t.Errorf("closing a code in %s was not reported", c.schema)
			}
		})
	}
}

// initialisms are the words a Go name writes in capitals: the common
// initialisms of Go's naming convention, as linters and generators use.
var initialisms = func() map[string]string {
	m := map[string]string{}
	for _, w := range strings.Fields("ACL API ASCII CPU CSS CSV DNS EOF GUID HTML HTTP HTTPS ID IP JSON LHS QPS RAM RHS RPC SLA SMTP SQL SSH TCP TLS TTL UDP UI UID UUID URI URL UTF8 VM XML XMPP XSRF XSS") {
		m[strings.ToLower(w)] = w
	}
	return m
}()

// goName returns the Go name of a constant of the named type for a
// snake_case value: invalid_json gives ErrorCodeInvalidJSON.
func goName(typeName, value string) string {
	var b strings.Builder
	b.WriteString(typeName)
	for _, w := range strings.Split(value, "_") {
		if up, ok := initialisms[w]; ok {
			b.WriteString(up)
		} else if w != "" {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	return b.String()
}

// TestCodeConstantNames holds every code constant's name to its value, so
// a constant callers already use keeps its name when the types are
// generated.
func TestCodeConstantNames(t *testing.T) {
	for _, typeName := range []string{"ErrorCode", "FieldErrorCode", "EventCategory"} {
		t.Run(typeName, func(t *testing.T) {
			decls := sdkConstDecls(t, typeName)
			if len(decls) == 0 {
				t.Fatalf("found no %s constants", typeName)
			}
			for _, d := range decls {
				if want := goName(typeName, d.value); d.name != want {
					t.Errorf("%s %q is named %s, want %s", typeName, d.value, d.name, want)
				}
			}
		})
	}
}

func TestGoName(t *testing.T) {
	for value, want := range map[string]string{
		"invalid_json":    "ErrorCodeInvalidJSON",
		"api_key_revoked": "ErrorCodeAPIKeyRevoked",
		"uri_too_long":    "ErrorCodeURITooLong",
		"not_found":       "ErrorCodeNotFound",
	} {
		if got := goName("ErrorCode", value); got != want {
			t.Errorf("goName(%q) = %s, want %s", value, got, want)
		}
	}
}
