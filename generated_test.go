package allisall

// These tests hold the generated core to the rules it is generated under.
// They read the package's own source, so they run in the public mirror as
// well as in the monorepo. That the committed files equal what the API's
// document generates is checked by the generator's --check, which needs
// the document and Node.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// parsePackage parses the package's non-test files, by file name.
func parsePackage(t *testing.T) map[string]*ast.File {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		out[name] = f
	}
	return out
}

func imports(f *ast.File) []string {
	var out []string
	for _, spec := range f.Imports {
		p, _ := strconv.Unquote(spec.Path.Value)
		out = append(out, p)
	}
	return out
}

func TestTheGeneratedFilesImportOnlyTime(t *testing.T) {
	files := parsePackage(t)
	for name, want := range map[string][]string{"types_gen.go": {"time"}, "operations_gen.go": nil} {
		f, ok := files[name]
		if !ok {
			t.Fatalf("%s is missing", name)
		}
		if got := imports(f); !slices.Equal(got, want) {
			t.Errorf("%s imports %v, want %v", name, got, want)
		}
	}
}

func TestTheModuleRequiresNoModule(t *testing.T) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) > 0 && (f[0] == "require" || f[0] == "replace" || f[0] == "tool") {
			t.Errorf("go.mod has %q; the module depends on the standard library only", line)
		}
	}
	if _, err := os.Stat("go.sum"); err == nil {
		t.Error("go.sum exists; the module depends on the standard library only")
	}
}

// declaredTypes returns the type names a file declares.
func declaredTypes(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			out = append(out, spec.(*ast.TypeSpec).Name.Name) // a type declaration holds only TypeSpecs
		}
	}
	return out
}

func TestTheGeneratedTypesLeaveTheHandWrittenOnesAlone(t *testing.T) {
	files := parsePackage(t)
	generated := declaredTypes(files["types_gen.go"])
	// Written by hand, or the SDK's own: never generated.
	for _, name := range []string{
		"ErrorCode", "FieldErrorCode", "EventCategory", "EventInput", "IngestRowError", "Problem", "FieldError",
		"EventBatch", "IngestResult", "ImportUpload", "Client",
		"Uuid", "UuidInput", "Instant", "InstantInput", "Date", "DateInput", "Month", "Currency", "MinorUnits", "Money", "Ratio",
	} {
		if slices.Contains(generated, name) {
			t.Errorf("types_gen.go declares %s", name)
		}
	}
	if !slices.Contains(generated, "ClientRecord") {
		t.Error("types_gen.go does not declare ClientRecord, the API's Client")
	}
	for _, name := range []string{"ErrorCode", "FieldErrorCode", "EventCategory", "EventInput", "IngestRowError", "Problem", "FieldError"} {
		found := false
		for file, f := range files {
			if file != "types_gen.go" && slices.Contains(declaredTypes(f), name) {
				found = true
			}
		}
		if !found {
			t.Errorf("no hand-written file declares %s", name)
		}
	}
}

// floatField is a wire member whose type names a binary float: its name,
// as Type.Field.Field, and the float type it names.
type floatField struct {
	name string
	typ  string
}

// floatFields returns every wire member, a struct field with a json tag,
// nested ones included, whose type names float32 or float64, sorted by
// name.
func floatFields(files map[string]*ast.File) []floatField {
	var out []floatField
	var walk func(prefix string, st *ast.StructType)
	walk = func(prefix string, st *ast.StructType) {
		for _, field := range st.Fields.List {
			if field.Tag == nil || !strings.Contains(field.Tag.Value, `json:"`) {
				continue
			}
			names := []string{}
			for _, n := range field.Names {
				names = append(names, n.Name)
			}
			if len(names) == 0 {
				names = []string{"(embedded)"}
			}
			for _, n := range names {
				ast.Inspect(field.Type, func(node ast.Node) bool {
					switch x := node.(type) {
					case *ast.StructType:
						walk(prefix+"."+n, x)
						return false
					case *ast.Ident:
						if x.Name == "float32" || x.Name == "float64" {
							out = append(out, floatField{name: prefix + "." + n, typ: x.Name})
						}
					}
					return true
				})
			}
		}
	}
	for _, f := range files {
		ast.Inspect(f, func(node ast.Node) bool {
			if ts, ok := node.(*ast.TypeSpec); ok {
				if st, ok := ts.Type.(*ast.StructType); ok {
					walk(ts.Name.Name, st)
				}
				return false
			}
			return true
		})
	}
	slices.SortFunc(out, func(a, b floatField) int { return strings.Compare(a.name, b.name) })
	return out
}

// floatMembers are the fields that may be binary floats: the API's plain
// numbers, which are quantities, ratios and points, never money. Money is
// a Decimal. A new float field fails this test until it is shown not to be
// money and listed here.
var floatMembers = []string{
	"AttributedEvent.Quantity",
	"Anomalies.UnmappedOutcomeTypes.Quantity",
	"EventInput.Quantity",
	"OutcomeTypeUsage.Quantity",
	"PortfolioClient.Roi",
	"PreviewEvent.Quantity",
	"PublicReport.ComparedTo.Roi",
	"Ratios.Roi",
	"Ratios.ValueToCost",
	"Renewal.Health.CurrentRoi",
	"Renewal.Health.DeltaPoints",
	"Renewal.Health.PriorRoi",
	"Renewal.Roi",
	"ValueLine.Quantity",
}

func TestNoMoneyIsAFloat(t *testing.T) {
	var got []string
	for _, f := range floatFields(parsePackage(t)) {
		got = append(got, f.name)
		// A listed member is a float64, as the API's number is; a float32
		// would round the value the API sent.
		if f.typ != "float64" {
			t.Errorf("%s is a %s, want float64", f.name, f.typ)
		}
	}
	want := slices.Sorted(slices.Values(floatMembers))
	if !slices.Equal(got, want) {
		t.Errorf("float fields:\n got %v\nwant %v", got, want)
	}
}

func TestFloatFieldsFindsNestedFields(t *testing.T) {
	src := "package p\n" +
		"type A struct {\n" +
		"	X float64 `json:\"x\"`\n" +
		"	Y *float32 `json:\"y\"`\n" +
		"	Z struct{ W []float64 `json:\"w\"`; V string `json:\"v\"` } `json:\"z\"`\n" +
		"	S string `json:\"s\"`\n" +
		"	r func() float64\n" +
		"}"
	f, err := parser.ParseFile(token.NewFileSet(), "a.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []floatField{{"A.X", "float64"}, {"A.Y", "float32"}, {"A.Z.W", "float64"}}
	if got := floatFields(map[string]*ast.File{"a.go": f}); !slices.Equal(got, want) {
		t.Errorf("floatFields = %v, want %v", got, want)
	}
}

// nullableMembers are the merge-patch members that can be cleared: the
// spec's nullable patch members, each a Nullable.
var nullableMembers = map[reflect.Type][]string{
	reflect.TypeFor[ClientPatch]():       {"client_key", "notes", "renewal_date"},
	reflect.TypeFor[AgentPatch]():        {"framework", "version"},
	reflect.TypeFor[CostLineItemPatch](): {"end_date"},
	reflect.TypeFor[ReportPatch]():       {"note"},
	reflect.TypeFor[ValueModelPatch]():   nil,
}

func TestPatchMembers(t *testing.T) {
	nullable := reflect.TypeFor[Nullable[string]]()
	for typ, want := range nullableMembers {
		var got []string
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			switch {
			case f.Type == nullable:
				got = append(got, name)
				// omitzero leaves out an absent member; omitempty would have
				// no effect on a struct, so it is not there.
				if opts != "omitzero" {
					t.Errorf("%s.%s is a Nullable tagged %q, want exactly omitzero", typ.Name(), f.Name, opts)
				}
			case f.Type.Kind() == reflect.Pointer, f.Type.Kind() == reflect.Slice, f.Type.Kind() == reflect.Map:
				if !slices.Contains(strings.Split(opts, ","), "omitempty") {
					t.Errorf("%s.%s is optional without omitempty, so absent is sent as null", typ.Name(), f.Name)
				}
			default:
				// A required member: always sent.
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s's Nullable members are %v, want %v", typ.Name(), got, want)
		}
	}
}

func TestTheOperationTable(t *testing.T) {
	table := operationTable()
	if len(table) != 48 {
		t.Errorf("the table holds %d operations, want 48", len(table))
	}
	var ids, idempotent []string
	for _, op := range table {
		ids = append(ids, op.id)
		if op.idempotent {
			idempotent = append(idempotent, op.id)
		}
		switch op.access {
		case accessKeyOnly, accessKeyOrSession:
			if op.scope == "" {
				t.Errorf("%s needs a key, but names no scope", op.id)
			}
		case accessPublic:
			if op.scope != "" {
				t.Errorf("%s takes no credential, but names a scope", op.id)
			}
		default:
			t.Errorf("%s has access %d, which the SDK does not call", op.id, op.access)
		}
		if op.status < 200 || op.status > 299 || op.method == "HEAD" || !strings.HasPrefix(op.path, "/") {
			t.Errorf("%s: %s %s answering %d is not an operation the SDK calls", op.id, op.method, op.path, op.status)
		}
	}
	if !slices.IsSorted(ids) || len(slices.Compact(slices.Clone(ids))) != len(ids) {
		t.Errorf("the ids are not sorted and unique: %v", ids)
	}
	for _, id := range []string{"getProblemType", "getSharedReport", "listApiKeys", "validateCalculation"} {
		if slices.Contains(ids, id) {
			t.Errorf("the table holds %s, which the SDK does not call", id)
		}
	}
	slices.Sort(idempotent)
	want := []string{"commitImport", "createAgent", "createCalculation", "createClient", "createCostLineItem", "createReport", "createValueModel"}
	if !slices.Equal(idempotent, want) {
		t.Errorf("idempotent operations = %v, want %v", idempotent, want)
	}
}

// exportedNames returns the package's exported names: each top-level
// declaration, and each method as Type.Method.
func exportedNames(files map[string]*ast.File) []string {
	var out []string
	for _, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Recv == nil {
					out = append(out, d.Name.Name)
					continue
				}
				recv := d.Recv.List[0].Type
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
				}
				switch index := recv.(type) {
				case *ast.IndexExpr: // one type parameter, as Nullable[T]
					recv = index.X
				case *ast.IndexListExpr: // several, as Page[T, B]
					recv = index.X
				}
				if id, ok := recv.(*ast.Ident); ok && id.IsExported() {
					out = append(out, id.Name+"."+d.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							out = append(out, s.Name.Name)
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								out = append(out, n.Name)
							}
						}
					}
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// TestExportedNames holds the package's exported names to a committed
// list, so a name a change or a regeneration adds or removes shows in
// review.
func TestExportedNames(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "exported_names.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(string(b))
	got := exportedNames(parsePackage(t))
	if onlyWant, onlyGot := sameSet(want, got); len(onlyWant) > 0 || len(onlyGot) > 0 {
		t.Errorf("exported names differ from testdata/exported_names.txt:\nonly in the list: %v\nonly in the package: %v", onlyWant, onlyGot)
	}
	if !slices.IsSorted(want) || len(slices.Compact(slices.Clone(want))) != len(want) {
		t.Error("testdata/exported_names.txt is not sorted and unique")
	}
}
