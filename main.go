package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

const (
	OutputTemplate = `{{printf "%-*s  %-*s  %-*s  %-*s  %s" .FilenameWidth "FILE" .KindWidth "KIND" .PositionWidth "POSITION" .TargetWidth "TARGET" "DETAIL"}}
{{range .Usages}}{{if .Filename}}{{printf "%-*s" $.FilenameWidth .Filename}}{{else}}{{spaces $.FilenameWidth}}{{end}}  {{printf "%-*s" $.KindWidth .Kind}}  {{printf "%-*s" $.PositionWidth .Position}}  {{printf "%-*s" $.TargetWidth .Target}}  {{.Detail}}
{{end}}
`
	PositionFmt = "%s:%d"

	NoPointerFoundMsg     = "(no pointer usage found)"
	TemplateParseErrorFmt = "template parse error: %v"
	TemplateExecErrorFmt  = "template execution error: %v"
	DirReadErrorFmt       = "directory read error: %v"
	ParseFailedFmt        = "parse failed: %v"
	NoGoFilesMsg          = "no .go files found in current directory"
)

type PointerUsage struct {
	Filename string
	Kind     string // "PointerType", "AddressOf", "Deref"
	Position string
	Target   string
	Detail   string
}

type PointerDetector struct {
	fset            *token.FileSet
	usages          []PointerUsage
	writer          io.Writer
	currentFilename string
}

func NewPointerDetector(fset *token.FileSet, w io.Writer) *PointerDetector {
	if w == nil {
		w = os.Stdout
	}
	return &PointerDetector{
		fset:   fset,
		usages: make([]PointerUsage, 0, 16),
		writer: w,
	}
}

func (d *PointerDetector) SetFilename(filename string) {
	d.currentFilename = filename
}

func (d *PointerDetector) Add(kind, target, detail string, pos token.Pos) {
	posStr := "-"
	if pos.IsValid() {
		p := d.fset.Position(pos)
		posStr = fmt.Sprintf(PositionFmt, filepath.Base(p.Filename), p.Line)
	}

	d.usages = append(d.usages, PointerUsage{
		Filename: d.currentFilename,
		Kind:     kind,
		Position: posStr,
		Target:   target,
		Detail:   detail,
	})
}

func (d *PointerDetector) calculateColumnWidths() (filenameWidth, kindWidth, positionWidth, targetWidth int) {
	filenameWidth = len("FILE")
	kindWidth = len("KIND")
	positionWidth = len("POSITION")
	targetWidth = len("TARGET")

	for _, u := range d.usages {
		if len(u.Filename) > filenameWidth {
			filenameWidth = len(u.Filename)
		}
		if len(u.Kind) > kindWidth {
			kindWidth = len(u.Kind)
		}
		if len(u.Position) > positionWidth {
			positionWidth = len(u.Position)
		}
		if len(u.Target) > targetWidth {
			targetWidth = len(u.Target)
		}
	}

	return filenameWidth, kindWidth, positionWidth, targetWidth
}

func (d *PointerDetector) PrintAll() {
	if len(d.usages) == 0 {
		fmt.Fprintln(d.writer, NoPointerFoundMsg)
		return
	}

	filenameWidth, kindWidth, positionWidth, targetWidth := d.calculateColumnWidths()

	tmpl := template.New("pointer_output").
		Funcs(template.FuncMap{
			"printf": fmt.Sprintf,
			"spaces": func(n int) string {
				return strings.Repeat(" ", n)
			},
		})

	tmpl, err := tmpl.Parse(OutputTemplate)
	if err != nil {
		fmt.Fprintf(d.writer, TemplateParseErrorFmt, err)
		return
	}

	var buf bytes.Buffer
	data := struct {
		Usages        []PointerUsage
		FilenameWidth int
		KindWidth     int
		PositionWidth int
		TargetWidth   int
	}{
		Usages:        d.usages,
		FilenameWidth: filenameWidth,
		KindWidth:     kindWidth,
		PositionWidth: positionWidth,
		TargetWidth:   targetWidth,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		fmt.Fprintf(d.writer, TemplateExecErrorFmt, err)
		return
	}

	fmt.Fprint(d.writer, buf.String())
}

// parentVisitor is a visitor that maintains a parent stack while walking the AST
type parentVisitor struct {
	d       *PointerDetector
	parents []ast.Node
}

func (v *parentVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	stack := make([]ast.Node, len(v.parents)+1)
	copy(stack, v.parents)
	stack[len(v.parents)] = n

	v.d.inspect(n, stack)

	return &parentVisitor{d: v.d, parents: stack}
}

func (d *PointerDetector) InspectWithParents(node ast.Node) {
	ast.Walk(&parentVisitor{d: d, parents: nil}, node)
}

func (d *PointerDetector) inspect(n ast.Node, parents []ast.Node) {
	switch curr := n.(type) {
	case *ast.StarExpr:
		if d.isTypeContextWithParents(parents) {
			target := exprToString(curr.X)
			d.Add("PointerType", target, "*"+target, curr.Pos())
		} else {
			target := exprToString(curr.X)
			d.Add("Deref", target, "*"+target, curr.Pos())
		}
	case *ast.UnaryExpr:
		if curr.Op == token.AND {
			target := exprToString(curr.X)
			d.Add("AddressOf", target, "&"+target, curr.OpPos)
		}
	}
}

func (d *PointerDetector) isTypeContextWithParents(parents []ast.Node) bool {
	for i := len(parents) - 1; i >= 0; i-- {
		switch parents[i].(type) {
		case *ast.ValueSpec, *ast.Field, *ast.FuncType,
			*ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.InterfaceType:
			return true
		}
	}
	return false
}

func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.UnaryExpr:
		return e.Op.String() + exprToString(e.X)
	case *ast.ParenExpr:
		return "(" + exprToString(e.X) + ")"
	case *ast.IndexExpr:
		return exprToString(e.X) + "[...]"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

type VisitorFunc func(ast.Node) ast.Visitor

func (f VisitorFunc) Visit(n ast.Node) ast.Visitor {
	return f(n)
}

func main() {
	fset := token.NewFileSet()

	files, err := os.ReadDir(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, DirReadErrorFmt, err)
		os.Exit(1)
	}

	var goFiles []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".go") {
			goFiles = append(goFiles, f.Name())
		}
	}

	if len(goFiles) == 0 {
		fmt.Println(NoGoFilesMsg)
		return
	}

	detector := NewPointerDetector(fset, os.Stdout)

	for _, filename := range goFiles {
		f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
		if err != nil {
			fmt.Fprintf(os.Stderr, ParseFailedFmt, err)
			continue
		}

		detector.SetFilename(filename)
		detector.InspectWithParents(f)
	}

	detector.PrintAll()

}
