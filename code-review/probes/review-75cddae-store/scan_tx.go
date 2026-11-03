// Command scan_tx is the reproducible static half of the store transaction
// review. It parses internal/store with go/ast and reports:
//
//	T1/T2  a s.DB.Begin() owner that reaches another Begin owner (self-deadlock
//	       on the single pooled connection),
//	T3     a Begin owner that still writes through s.DB instead of tx,
//	T4     a tx *sql.Tx helper that escapes to s.DB,
//	T5     a tx helper that calls a Begin-owning Store method,
//	T6     goroutines launched inside the package,
//	T7/T8  a Begin owner without defer tx.Rollback() (the transaction leaks and,
//	       at SetMaxOpenConns(1), every later query on that Store hangs).
//
// Run from code-review/probes:
//
//	go run ./review-75cddae-store/scan_tx.go ../../internal/store
//
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fn struct {
	key      string
	name     string
	file     string
	line     int
	recv     string
	ownBegin bool
	usesTXP  bool
	selCalls map[string]int
	plain    map[string]int
	dbLines  []int
	beginLn  int
	goLaunch []int
	rollback []int
	commit   []int
	sawDefer bool
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return recvName(t.X)
	}
	return ""
}

func main() {
	dir := os.Args[1]
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		fmt.Println("parse error:", err)
		os.Exit(1)
	}
	funcs := map[string]*fn{}
	var order []string
	for _, p := range pkgs {
		for name, file := range p.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				d, ok := n.(*ast.FuncDecl)
				if !ok || d.Body == nil {
					return true
				}
				r := ""
				if d.Recv != nil && len(d.Recv.List) > 0 {
					r = recvName(d.Recv.List[0].Type)
				}
				f := &fn{
					name:     d.Name.Name,
					file:     filepath.Base(name),
					line:     fset.Position(d.Pos()).Line,
					recv:     r,
					selCalls: map[string]int{},
					plain:    map[string]int{},
				}
				if r != "" {
					f.key = r + "." + d.Name.Name
				} else {
					f.key = d.Name.Name
				}
				if d.Type.Params != nil {
					for _, p := range d.Type.Params.List {
						if st, ok := p.Type.(*ast.StarExpr); ok {
							if se, ok := st.X.(*ast.SelectorExpr); ok && se.Sel.Name == "Tx" {
								f.usesTXP = true
							}
						}
					}
				}
				// pass 1: s.DB references (any SelectorExpr s.DB)
				ast.Inspect(d.Body, func(m ast.Node) bool {
					se, ok := m.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if id, ok := se.X.(*ast.Ident); ok && id.Name == "s" && se.Sel.Name == "DB" {
						f.dbLines = append(f.dbLines, fset.Position(se.Pos()).Line)
					}
					return true
				})
				// pass 2: calls, begin, rollback, commit, go
				ast.Inspect(d.Body, func(m ast.Node) bool {
					switch t := m.(type) {
					case *ast.GoStmt:
						f.goLaunch = append(f.goLaunch, fset.Position(t.Pos()).Line)
					case *ast.DeferStmt:
						ast.Inspect(t.Call, func(x ast.Node) bool {
							if ce, ok := x.(*ast.CallExpr); ok {
								if se, ok := ce.Fun.(*ast.SelectorExpr); ok && se.Sel.Name == "Rollback" {
									f.rollback = append(f.rollback, fset.Position(ce.Pos()).Line)
									f.sawDefer = true
								}
							}
							return true
						})
					case *ast.CallExpr:
						if se, ok := t.Fun.(*ast.SelectorExpr); ok {
							if se.Sel.Name == "Begin" {
								if inner, ok := se.X.(*ast.SelectorExpr); ok {
									if id, ok := inner.X.(*ast.Ident); ok && id.Name == "s" && inner.Sel.Name == "DB" {
										f.ownBegin = true
										if f.beginLn == 0 {
											f.beginLn = fset.Position(t.Pos()).Line
										}
									}
								}
							}
							if se.Sel.Name == "Commit" {
								f.commit = append(f.commit, fset.Position(t.Pos()).Line)
							}
							if id, ok := se.X.(*ast.Ident); ok && id.Name == "s" && se.Sel.Name != "DB" {
								f.selCalls[se.Sel.Name]++
							}
						} else if id, ok := t.Fun.(*ast.Ident); ok {
							f.plain[id.Name]++
						}
					}
					return true
				})
				funcs[f.key] = f
				order = append(order, f.key)
				return true
			})
		}
	}
	sort.Strings(order)
	beginning := map[string]bool{}
	for k, f := range funcs {
		if f.ownBegin {
			beginning[k] = true
		}
	}
	fmt.Printf("== scanned %d funcs (%d own s.DB.Begin()) ==\n", len(funcs), len(beginning))

	resolve := func(from *fn, name string) *fn {
		if from.recv != "" {
			if g := funcs[from.recv+"."+name]; g != nil {
				return g
			}
		}
		if g := funcs["Store."+name]; g != nil {
			return g
		}
		if g := funcs[name]; g != nil {
			return g
		}
		return nil
	}
	children := func(f *fn) []string {
		var out []string
		for n := range f.selCalls {
			out = append(out, n)
		}
		for n := range f.plain {
			out = append(out, n)
		}
		sort.Strings(out)
		return out
	}

	fmt.Println("\n== [T1/T2] Begin-owning func reaching another Begin-owning func (self-deadlock) ==")
	hits := 0
	for _, k := range order {
		f := funcs[k]
		if !f.ownBegin {
			continue
		}
		seen := map[string]bool{}
		var walk func(cur *fn, path []string) [][]string
		walk = func(cur *fn, path []string) [][]string {
			var res [][]string
			for _, n := range children(cur) {
				c := resolve(cur, n)
				if c == nil || c.key == cur.key || seen[c.key] {
					continue
				}
				seen[c.key] = true
				np := append(append([]string{}, path...), c.key)
				if c.ownBegin {
					res = append(res, np)
					continue
				}
				res = append(res, walk(c, np)...)
			}
			return res
		}
		paths := walk(f, []string{k})
		if len(paths) > 0 {
			hits++
			fmt.Printf("  %s (%s:%d)\n", k, f.file, f.line)
			for _, p := range paths {
				fmt.Printf("     -> %s\n", strings.Join(p, " => "))
			}
		}
	}
	if hits == 0 {
		fmt.Println("  (none)")
	}

	fmt.Println("\n== [T3] Begin-owning func that still touches s.DB (must use tx only) ==")
	n3 := 0
	for _, k := range order {
		f := funcs[k]
		if !f.ownBegin {
			continue
		}
		var bad []int
		for _, ln := range f.dbLines {
			if ln != f.beginLn {
				bad = append(bad, ln)
			}
		}
		if len(bad) > 0 {
			n3++
			sort.Ints(bad)
			fmt.Printf("  %-36s %s:%d  s.DB@%v  begin@%d\n", k, f.file, f.line, bad, f.beginLn)
		}
	}
	if n3 == 0 {
		fmt.Println("  (none)")
	}

	fmt.Println("\n== [T4] tx *sql.Tx helper referencing s.DB (escapes the tx) ==")
	n4 := 0
	for _, k := range order {
		f := funcs[k]
		if !f.usesTXP || len(f.dbLines) == 0 {
			continue
		}
		n4++
		fmt.Printf("  %-36s %s:%d s.DB@%v\n", k, f.file, f.line, f.dbLines)
	}
	if n4 == 0 {
		fmt.Println("  (none)")
	}

	fmt.Println("\n== [T5] tx *sql.Tx helper calling a Begin-owning Store method ==")
	n5 := 0
	for _, k := range order {
		f := funcs[k]
		if !f.usesTXP || f.ownBegin {
			continue
		}
		var bad []string
		for _, n := range children(f) {
			c := resolve(f, n)
			if c != nil && c.ownBegin && c.key != f.key {
				bad = append(bad, c.key)
			}
		}
		if len(bad) > 0 {
			n5++
			fmt.Printf("  %-36s %s:%d -> %s\n", k, f.file, f.line, strings.Join(bad, ", "))
		}
	}
	if n5 == 0 {
		fmt.Println("  (none)")
	}

	fmt.Println("\n== [T6] funcs launching goroutines ==")
	n6 := 0
	for _, k := range order {
		f := funcs[k]
		if len(f.goLaunch) > 0 {
			n6++
			fmt.Printf("  %-36s %s:%d go@%v\n", k, f.file, f.line, f.goLaunch)
		}
	}
	if n6 == 0 {
		fmt.Println("  (none)")
	}

	fmt.Println("\n== [T7] Begin-owning func WITHOUT defer tx.Rollback() (tx leak => permanent hang at MaxOpenConns(1)) ==")
	n7 := 0
	for _, k := range order {
		f := funcs[k]
		if !f.ownBegin {
			continue
		}
		if len(f.rollback) == 0 {
			n7++
			fmt.Printf("  %-36s %s:%d commit@%v\n", k, f.file, f.line, f.commit)
		}
	}
	if n7 == 0 {
		fmt.Println("  (none)")
	}

	fmt.Println("\n== [T8] Begin-owning func whose first statement is not a deferred Rollback (ordered audit) ==")
	for _, k := range order {
		f := funcs[k]
		if !f.ownBegin || len(f.rollback) == 0 {
			continue
		}
		rb := f.rollback[0]
		var early []int
		for _, c := range f.commit {
			if c < rb {
				early = append(early, c)
			}
		}
		if len(early) > 0 {
			fmt.Printf("  %-36s %s:%d commit before deferred rollback: %v\n", k, f.file, f.line, early)
		}
	}
	fmt.Println("  (checked)")
}
