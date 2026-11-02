package allisall

// These tests type-check the README's examples against this package, as
// written. They are checked, never run, so none of them reaches the
// network.

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	goBlock = regexp.MustCompile("(?s)```go\n(.*?)```")
	// A whole program's package clause, after any leading comment lines.
	packageClause = regexp.MustCompile(`^(?:\s*//[^\n]*\n)*\s*package `)
)

// readmeExamples returns the README's go blocks.
func readmeExamples(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	var out []string
	for _, m := range goBlock.FindAllStringSubmatch(strings.ReplaceAll(string(b), "\r\n", "\n"), -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatal("README.md has no go examples")
	}
	return out
}

// exampleImports are the packages a README fragment may name without
// importing them, by the name it uses.
var exampleImports = map[string]string{
	"allisall": "github.com/All-is-AI/allisall-go",
	"context":  "context",
	"errors":   "errors",
	"fmt":      "fmt",
	"http":     "net/http",
	"log":      "log",
	"os":       "os",
	"time":     "time",
}

// asFile turns a README example into a Go file, and reports whether it
// is a fragment. A whole program is kept as written. An import alone is
// put under a package clause. Any other block is a fragment: it becomes a
// block inside a function that returns an error, with an import for each
// package in exampleImports. The function declares what a fragment may
// use without declaring it: err, ctx, client and clientID. The block
// lets a fragment declare its own client or err with :=.
func asFile(example string) (src string, fragment bool) {
	switch {
	case packageClause.MatchString(example):
		return example, false
	case strings.HasPrefix(example, "import "):
		return "package example\n\n" + example, true
	}
	var imports []string
	for name, path := range exampleImports {
		imports = append(imports, name+` "`+path+`"`)
	}
	slices.Sort(imports)
	return "package example\n\nimport (\n" + strings.Join(imports, "\n") +
		"\n)\n\nfunc _(ctx context.Context, client *allisall.Client, clientID string) error {\n" +
		"\tvar err error\n\t{\n" + example + "\t}\n\treturn err\n}\n", true
}

// A checker type-checks examples against this package's source and the
// standard library. Its importer keeps each package it has checked, so
// the examples share that work.
type checker struct {
	fset     *token.FileSet
	importer types.Importer
}

func newChecker() *checker {
	fset := token.NewFileSet()
	return &checker{fset, importer.ForCompiler(fset, "source", nil)}
}

// typeCheck type-checks src. For a fragment, a name or import it declares
// and does not use is not an error: a fragment shows a step, not a whole
// program.
func (c *checker) typeCheck(src string, fragment bool) []string {
	f, err := parser.ParseFile(c.fset, "example.go", src, 0)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	conf := types.Config{
		Importer: c.importer,
		Error: func(err error) {
			msg := err.Error()
			if fragment && strings.Contains(msg, "and not used") {
				return
			}
			if fragment && strings.Contains(msg, "undefined: ") {
				msg += " (a package a fragment names must be in exampleImports)"
			}
			problems = append(problems, msg)
		},
	}
	_, _ = conf.Check("example", c.fset, []*ast.File{f}, nil) // problems holds every error
	return problems
}

func TestReadmeExamplesTypeCheck(t *testing.T) {
	examples := readmeExamples(t)
	c := newChecker()
	whole := 0
	for i, example := range examples {
		src, fragment := asFile(example)
		if !fragment {
			whole++
		}
		if problems := c.typeCheck(src, fragment); len(problems) > 0 {
			t.Errorf("README go example %d:\n%s", i+1, strings.Join(problems, "\n"))
		}
	}
	if whole == 0 {
		t.Error("README.md has no whole go program")
	}
}

// TestReadmeCheckCanFail checks that the type check reports a misnamed
// field, in a whole program and in a fragment, so the test above cannot
// pass on a checker that never read this package.
func TestReadmeCheckCanFail(t *testing.T) {
	c := newChecker()
	for _, example := range []string{
		"package main\n\nimport \"github.com/All-is-AI/allisall-go\"\n\nfunc main() { _ = allisall.IngestSummary{}.Acceptd }\n",
		"var summary allisall.IngestSummary\n_ = summary.Acceptd\n",
	} {
		if problems := c.typeCheck(asFile(example)); !slices.ContainsFunc(problems, func(p string) bool {
			return strings.Contains(p, "Acceptd")
		}) {
			t.Errorf("a misnamed field was not reported: %v", problems)
		}
	}
}

func TestAsFileKeepsAWholeProgram(t *testing.T) {
	for _, example := range []string{
		"package main\n\nfunc main() {}\n",
		"// main.go\npackage main\n\nfunc main() {}\n",
	} {
		if src, fragment := asFile(example); fragment || src != example {
			t.Errorf("asFile(%q) treated a whole program as a fragment", example)
		}
	}
	if _, fragment := asFile("x := 1\n_ = x\n"); !fragment {
		t.Error("asFile treated a fragment as a whole program")
	}
}
