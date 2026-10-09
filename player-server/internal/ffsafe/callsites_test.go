package ffsafe

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file checks, on the syntax tree of the server's own sources, that
// every place a process is created gets its arguments from this package.
// What is enforced, exactly:
//
// A "command site" is a reference to Command or CommandContext of os/exec
// (under whatever name the file imports it; a call or the bare function
// value), or an exec.Cmd composite literal. A function declaration
// containing a command site is accepted when
//
//	(a) it calls InputArgs, SourceArgs or ImageInputArgs of this package, or
//	(b) it calls a function of its own package that does (a), or
//	(c) it is called from its own package and every function calling it
//	    satisfies (a) or (b) — it runs the arguments its callers built.
//
// Always rejected, wherever they are: a command site outside a function
// declaration, a dot-import of os/exec, and any use of os.StartProcess or
// of syscall.Exec, ForkExec or StartProcess. Exceptions need an entry in
// commandAllowlist.
//
// Limits, deliberately: functions are matched by name within a package
// (methods of different types with one name count as one function); the
// check does not follow the data flow, so it cannot tell whether the ffsafe
// arguments are the ones passed on, nor see a command created through a
// stored function value (thumb.FFmpegGenerator.execer). Callers are looked
// for in the function's own package only, so rule (c) does not see another
// package calling an exported function that runs prebuilt arguments; keep
// such runners unexported. A command built with new(exec.Cmd) or a zero
// exec.Cmd variable is not a command site either. Those are covered
// by the argument-list tests: TestProbeArgs, TestRemuxArgs, TestThumbArgs
// and TestFFmpegArgs.

// hardeningFuncs are the functions of this package that build input
// arguments.
var hardeningFuncs = []string{"InputArgs", "SourceArgs", "ImageInputArgs"}

// commandAllowlist names the functions ("<file path below player-server>:
// <function>") that may create a process without ffsafe arguments, with the
// reason. An entry that no longer matches anything fails the test.
var commandAllowlist = map[string]string{
	"internal/ffsafe/ffsafetest/ffsafetest.go:Generate": "test support: writes fixtures from ffmpeg's synthetic sources, reads no user media",
}

const (
	execImport   = "os/exec"
	ffsafeImport = "codeberg.org/snonux/player/internal/ffsafe"
)

// pkgFile is one parsed source file of a package.
type pkgFile struct {
	path string // as reported in findings
	ast  *ast.File
}

// funcFacts is what the check knows about the functions of one name in a
// package.
type funcFacts struct {
	sites    []string        // files in which the function has a command site
	hardened bool            // rule (a): calls a hardening function
	calls    map[string]bool // names of the functions it calls
}

// importNames returns the names under which file refers to the package
// path: its last element, or the aliases given. "." and "_" are returned as
// such.
func importNames(file *ast.File, path string) []string {
	var names []string
	for _, imp := range file.Imports {
		if got, err := strconv.Unquote(imp.Path.Value); err != nil || got != path {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names = append(names, name)
	}
	return names
}

// isSelector reports whether n is "<one of pkgs>.<one of names>".
func isSelector(n ast.Node, pkgs []string, names ...string) bool {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains(pkgs, pkg.Name) && slices.Contains(names, sel.Sel.Name)
}

// fileScanner collects the facts of one file.
type fileScanner struct {
	file      pkgFile
	exec      []string // local names of os/exec
	ffsafe    []string // local names of this package
	facts     map[string]*funcFacts
	forbidden []string // findings that no rule can accept
}

// inspect records what node, found inside the function named fn ("" outside
// any function declaration), contributes.
func (s *fileScanner) inspect(fn string, node ast.Node) {
	facts := s.facts[fn]
	if facts == nil {
		facts = &funcFacts{calls: map[string]bool{}}
		s.facts[fn] = facts
	}
	if lit, ok := node.(*ast.CompositeLit); ok && isSelector(lit.Type, s.exec, "Cmd") {
		facts.sites = append(facts.sites, s.file.path)
	}
	if isSelector(node, s.exec, "Command", "CommandContext") {
		facts.sites = append(facts.sites, s.file.path)
	}
	if isSelector(node, importNames(s.file.ast, "os"), "StartProcess") ||
		isSelector(node, importNames(s.file.ast, "syscall"), "Exec", "ForkExec", "StartProcess") {
		s.forbidden = append(s.forbidden, fmt.Sprintf("%s:%s starts a process below os/exec", s.file.path, fn))
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		facts.calls[fun.Name] = true
	case *ast.SelectorExpr:
		if isSelector(fun, s.ffsafe, hardeningFuncs...) {
			facts.hardened = true
		} else {
			facts.calls[fun.Sel.Name] = true // a method, or another package's function
		}
	}
}

// scan walks the whole file, attributing every node to the function
// declaration it is in.
func (s *fileScanner) scan() {
	if slices.Contains(s.exec, ".") {
		s.forbidden = append(s.forbidden, s.file.path+" dot-imports os/exec")
	}
	for _, decl := range s.file.ast.Decls {
		name := ""
		if fn, ok := decl.(*ast.FuncDecl); ok {
			name = fn.Name.Name
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			if n != nil {
				s.inspect(name, n)
			}
			return true
		})
	}
}

// checkPackage applies the rules at the top of this file to the files of
// one package and returns the findings, sorted. used receives the allowlist
// keys that excused something.
func checkPackage(files []pkgFile, allow map[string]string, used map[string]bool) []string {
	facts := map[string]*funcFacts{}
	var findings []string
	for _, file := range files {
		s := &fileScanner{file: file, facts: facts}
		s.exec = importNames(file.ast, execImport)
		s.ffsafe = importNames(file.ast, ffsafeImport)
		s.scan()
		findings = append(findings, s.forbidden...)
	}
	linked := func(name string) bool { // rules (a) and (b)
		f := facts[name]
		if f == nil {
			return false
		}
		for callee := range f.calls {
			if c := facts[callee]; c != nil && c.hardened {
				return true
			}
		}
		return f.hardened
	}
	for name, f := range facts {
		for _, path := range f.sites {
			key := path + ":" + name
			switch {
			case name == "":
				findings = append(findings, path+" creates a command outside a function")
			case linked(name) || fedByLinkedCallers(name, facts, linked):
			case allow[key] != "":
				used[key] = true
			default:
				findings = append(findings, key+" creates a command without arguments from ffsafe")
			}
		}
	}
	sort.Strings(findings)
	return slices.Compact(findings)
}

// fedByLinkedCallers is rule (c): name has callers in the package and each
// of them is linked.
func fedByLinkedCallers(name string, facts map[string]*funcFacts, linked func(string) bool) bool {
	callers := 0
	for caller, f := range facts {
		if caller == name || !f.calls[name] {
			continue
		}
		if !linked(caller) {
			return false
		}
		callers++
	}
	return callers > 0
}

// parsePackages parses every non-test Go file below root, grouped by
// directory. Paths in the result are relative to root, slash-separated.
func parsePackages(t *testing.T, root string) map[string][]pkgFile {
	t.Helper()
	fset := token.NewFileSet()
	pkgs := map[string][]pkgFile{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		pkgs[dir] = append(pkgs[dir], pkgFile{path: filepath.ToSlash(rel), ast: parsed})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return pkgs
}

// TestEveryCommandGetsFFsafeArguments applies the check to all of
// player-server. It needs no ffmpeg.
func TestEveryCommandGetsFFsafeArguments(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("server sources not found at %s: %v", root, err)
	}
	used := map[string]bool{}
	sites := 0
	for _, files := range parsePackages(t, root) {
		for _, finding := range checkPackage(files, commandAllowlist, used) {
			t.Error(finding)
		}
		for _, file := range files {
			if len(importNames(file.ast, execImport)) > 0 {
				sites++
			}
		}
	}
	for key := range commandAllowlist {
		if !used[key] {
			t.Errorf("allowlist entry %q matches no command site any more; remove it", key)
		}
	}
	// probe.go, remux.go, thumb.go, transcode/ffmpeg.go, transcode/ffprobe.go
	// and ffsafetest.go import os/exec today.
	if sites < 6 {
		t.Errorf("only %d files import os/exec: is the walk root wrong?", sites)
	}
}

// TestCheckPackage runs the checker on small in-memory packages: each case
// is one or more files, and the findings they must produce.
func TestCheckPackage(t *testing.T) {
	const header = "package p\nimport (\n\"os/exec\"\n\"codeberg.org/snonux/player/internal/ffsafe\"\n)\nvar _ = ffsafe.FormatWhitelist\n"
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{
			name:  "bare call",
			files: []string{"package p\nimport \"os/exec\"\nfunc Probe(p string) { exec.Command(\"ffprobe\", p).Run() }"},
			want:  []string{"f0.go:Probe creates a command without arguments from ffsafe"},
		},
		{
			name:  "aliased import",
			files: []string{"package p\nimport run \"os/exec\"\nfunc Probe(p string) { run.CommandContext(nil, \"ffprobe\", p).Run() }"},
			want:  []string{"f0.go:Probe creates a command without arguments from ffsafe"},
		},
		{
			name: "ffsafe mentioned only in a comment",
			files: []string{"package p\nimport \"os/exec\"\n// Probe uses ffsafe.InputArgs.\n" +
				"func Probe(p string) { /* ffsafe.InputArgs(p) */ exec.Command(\"ffprobe\", p).Run() }"},
			want: []string{"f0.go:Probe creates a command without arguments from ffsafe"},
		},
		{
			name:  "ffsafe used elsewhere in the file only",
			files: []string{header + "func Good(p string) { a, _ := ffsafe.InputArgs(p); exec.Command(\"ffprobe\", a...).Run() }\nfunc Bad(p string) { exec.Command(\"ffprobe\", p).Run() }"},
			want:  []string{"f0.go:Bad creates a command without arguments from ffsafe"},
		},
		{
			name:  "function value and struct literal",
			files: []string{"package p\nimport \"os/exec\"\nfunc A() any { return exec.CommandContext }\nfunc B() any { return &exec.Cmd{Path: \"ffmpeg\"} }"},
			want: []string{
				"f0.go:A creates a command without arguments from ffsafe",
				"f0.go:B creates a command without arguments from ffsafe",
			},
		},
		{
			name:  "outside a function, dot import, os.StartProcess",
			files: []string{"package p\nimport (\n\"os\"\n. \"os/exec\"\nx \"os/exec\"\n)\nvar c = x.Command(\"ffmpeg\")\nfunc S() { os.StartProcess(\"ffmpeg\", nil, nil) }"},
			want: []string{
				"f0.go creates a command outside a function",
				"f0.go dot-imports os/exec",
				"f0.go:S starts a process below os/exec",
			},
		},
		{
			name:  "rule a: direct use",
			files: []string{header + "func Probe(p string) { a, _ := ffsafe.SourceArgs(p); exec.Command(\"ffprobe\", a...).Run() }"},
		},
		{
			name: "rule b: helper in another file of the package",
			files: []string{
				"package p\nimport \"os/exec\"\nfunc Probe(p string) { exec.Command(\"ffprobe\", args(p)...).Run() }",
				"package p\nimport safe \"codeberg.org/snonux/player/internal/ffsafe\"\nfunc args(p string) []string { a, _ := safe.ImageInputArgs(p); return a }",
			},
		},
		{
			name: "rule c: runs what its callers built",
			files: []string{header + "type R struct{}\nfunc (R) run(a []string) { exec.Command(\"ffmpeg\", a...).Run() }\n" +
				"func (r R) Transcode(p string) { a, _ := ffsafe.InputArgs(p); r.run(a) }"},
		},
		{
			name: "rule c fails with one unlinked caller",
			files: []string{header + "func run(a []string) { exec.Command(\"ffmpeg\", a...).Run() }\n" +
				"func Good(p string) { a, _ := ffsafe.InputArgs(p); run(a) }\nfunc Bad(p string) { run([]string{\"-i\", p}) }"},
			want: []string{"f0.go:run creates a command without arguments from ffsafe"},
		},
		{
			name:  "rule c needs a caller",
			files: []string{"package p\nimport \"os/exec\"\nfunc run(a []string) { exec.Command(\"ffmpeg\", a...).Run() }"},
			want:  []string{"f0.go:run creates a command without arguments from ffsafe"},
		},
		{
			name:  "another package's InputArgs does not count",
			files: []string{"package p\nimport (\n\"os/exec\"\n\"example.com/other/ffsafe\"\n)\nfunc Probe(p string) { a, _ := ffsafe.InputArgs(p); exec.Command(\"ffprobe\", a...).Run() }"},
			want:  []string{"f0.go:Probe creates a command without arguments from ffsafe"},
		},
		{
			name:  "LookPath and the Cmd type are no command sites",
			files: []string{"package p\nimport \"os/exec\"\nfunc Has() bool { _, err := exec.LookPath(\"ffmpeg\"); return err == nil }\nfunc Wait(c *exec.Cmd) error { return c.Wait() }"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var files []pkgFile
			for i, src := range tt.files {
				name := fmt.Sprintf("f%d.go", i)
				parsed, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
				if err != nil {
					t.Fatalf("snippet does not parse: %v", err)
				}
				files = append(files, pkgFile{path: name, ast: parsed})
			}
			got := checkPackage(files, nil, map[string]bool{})
			if !slices.Equal(got, tt.want) {
				t.Errorf("findings = %q, want %q", got, tt.want)
			}
		})
	}
}

// An allowlisted function is excused, and the entry is reported as used.
func TestCheckPackage_Allowlist(t *testing.T) {
	const src = "package p\nimport \"os/exec\"\nfunc Nice() { exec.Command(\"nice\").Run() }"
	parsed, err := parser.ParseFile(token.NewFileSet(), "n.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	allow := map[string]string{"n.go:Nice": "not ffmpeg"}
	if got := checkPackage([]pkgFile{{path: "n.go", ast: parsed}}, allow, used); len(got) != 0 || !used["n.go:Nice"] {
		t.Errorf("findings = %q, used = %v; want none and the entry used", got, used)
	}
}
