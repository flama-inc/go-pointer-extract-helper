package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

const (
	// Test case names
	TestNamePointerDeclOnly  = "pointer type declaration only"
	TestNameAddressOf        = "address of (&)"
	TestNameDeref            = "dereference (*)"
	TestNameStructFieldDeref = "dereference via struct field"
	TestNameNoPointer        = "code without pointers"
	TestNameComplexCase      = "complex case (declaration + & + *)"

	TestNameVarDeclPointer         = "pointer type in variable declaration"
	TestNameFuncParamPointer       = "pointer type in function parameter"
	TestNameFuncReturnPointer      = "pointer type in function return"
	TestNameStructFieldPointer     = "pointer type in struct field"
	TestNameMapValuePointer        = "pointer as map value type"
	TestNameSliceElemPointer       = "pointer as slice element type"
	TestNameSimpleDeref            = "simple dereference expression"
	TestNameDerefWithSelector      = "dereference + selector"
	TestNameNestedArrayPointer     = "nested type declaration (array of pointers)"
	TestNameAddrAndDerefMixed      = "mixed address of and dereference"
	TestNameNoFalseDerefInTypeDecl = "no dereference should appear in type declaration"

	TestNameNestedTypeContext       = "nested type context"
	TestNameNoFalseDerefInTypeDecl2 = "Deref should not be falsely detected in type declaration"

	// Error message fragments
	ErrMsgParseFailed            = "parse error: %v"
	ErrMsgUnexpectedPointerCount = "PointerType count: got %d, want %d"
	ErrMsgUnexpectedDerefCount   = "Deref count: got %d, want %d"
	ErrMsgUnexpectedAddrCount    = "AddressOf count: got %d, want %d"
	ErrMsgFalseDerefDetected     = "Deref falsely detected in type declaration: %v"
	ErrMsgUnexpectedPointerTotal = "expected PointerType count = %d, got = %d"
	ErrMsgWriterNil              = "writer is still nil"

	// Test file name generation
	TestFilePrefix = "test_"
	TestFileExt    = ".go"

	// Expected values
	WantPointerCountNested        = 3
	WantPointerCountGlobalWrapper = 2

	// Test output (should match NoPointerFoundMsg)
	NoPointerFoundPartial = "no pointer usage found"
)

func TestPointerDetector_VariousCases(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		want    []string
		wantErr bool
	}{
		{
			name: TestNamePointerDeclOnly,
			source: `
package example

type Person struct {
	Name string
}

var p *Person
`,
			want: []string{
				"PointerType",
				"Person",
				"*Person",
			},
		},
		{
			name: TestNameAddressOf,
			source: `
package example

func main() {
	x := 42
	ptr := &x
	_ = ptr
}
`,
			want: []string{
				"AddressOf",
				"x",
				"&x",
			},
		},
		{
			name: TestNameDeref,
			source: `
package example

func main() {
	var ptr *int
	*ptr = 123
}
`,
			want: []string{
				"PointerType",
				"int",
				"*int",
				"Deref",
				"ptr",
				"*ptr",
			},
		},
		{
			name: TestNameStructFieldDeref,
			source: `
package example

type User struct { Score int }

func get(u *User) int {
	return (*u).Score
}
`,
			want: []string{
				"PointerType",
				"User",
				"*User",
				"Deref",
				"u",
				"*u",
			},
		},
		{
			name: TestNameNoPointer,
			source: `
package example

var x int = 10
func main() {
	y := x + 5
}
`,
			want: []string{},
		},
		{
			name: TestNameComplexCase,
			source: `
package example

import "fmt"

type Config struct {
	timeout *int
}

func NewConfig() *Config {
	t := 30
	return &Config{timeout: &t}
}

func (c *Config) Timeout() int {
	if c.timeout == nil {
		return 0
	}
	return *c.timeout
}
`,
			want: []string{
				"PointerType",
				"int",
				"AddressOf",
				"t",
				"&t",
				"PointerType",
				"Config",
				"Deref",
				"c.timeout",
				"*c.timeout",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()

			f, err := parser.ParseFile(fset, TestFilePrefix+tt.name+TestFileExt, tt.source, 0)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseFile error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			var buf bytes.Buffer
			detector := NewPointerDetector(fset, &buf)

			detector.SetFilename(TestFilePrefix + tt.name + TestFileExt)
			detector.InspectWithParents(f)
			detector.PrintAll()

			got := buf.String()

			// For NDJSON format, check if JSON contains the expected values
			for _, expected := range tt.want {
				// Handle JSON escaping: & becomes \u0026
				expectedEscaped := strings.ReplaceAll(expected, "&", "\\u0026")
				if !strings.Contains(got, expected) && !strings.Contains(got, expectedEscaped) {
					t.Errorf("expected string %q not found\noutput:\n%s", expected, got)
				}
			}

			if len(tt.want) == 0 && got != "" && !strings.Contains(got, NoPointerFoundPartial) {
				t.Errorf("output exists even though no pointer usage:\n%s", got)
			}
		})
	}
}

func TestParentTraversal_TypeContexts(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		wantPointer   int
		wantDeref     int
		wantAddressOf int
	}{
		{
			name: TestNameVarDeclPointer,
			source: `
package p
var x *int
`,
			wantPointer: 1,
			wantDeref:   0,
		},
		{
			name: TestNameFuncParamPointer,
			source: `
package p
func f(ptr *string) {}
`,
			wantPointer: 1,
			wantDeref:   0,
		},
		{
			name: TestNameFuncReturnPointer,
			source: `
package p
func g() *bool { return nil }
`,
			wantPointer: 1,
			wantDeref:   0,
		},
		{
			name: TestNameStructFieldPointer,
			source: `
package p
type S struct {
    data *float64
}
`,
			wantPointer: 1,
			wantDeref:   0,
		},
		{
			name: TestNameMapValuePointer,
			source: `
package p
var m map[string]*User
`,
			wantPointer: 1,
			wantDeref:   0,
		},
		{
			name: TestNameSliceElemPointer,
			source: `
package p
var items []*Item
`,
			wantPointer: 1,
			wantDeref:   0,
		},
		{
			name: TestNameSimpleDeref,
			source: `
package p
func main() {
    var p *int
    v := *p
}
`,
			wantPointer: 1,
			wantDeref:   1,
		},
		{
			name: TestNameDerefWithSelector,
			source: `
package p
type Person struct { Age int }
func main() {
    var pp *Person
    age := (*pp).Age
}
`,
			wantPointer: 1,
			wantDeref:   1,
		},
		{
			name: TestNameNestedArrayPointer,
			source: `
package p
var nested [][3]*string
`,
			wantPointer: 1,
			wantDeref:   0,
		},
		{
			name: TestNameAddrAndDerefMixed,
			source: `
package p
func main() {
    x := 42
    ptr := &x
    v := *ptr
}
`,
			wantPointer:   0,
			wantDeref:     1,
			wantAddressOf: 1,
		},
		{
			name: TestNameNoFalseDerefInTypeDecl,
			source: `
package p
type T struct {
    ptr *int
}
func f(t T) {
    _ = t.ptr
}
`,
			wantPointer:   1,
			wantDeref:     0,
			wantAddressOf: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, TestFilePrefix+tt.name+TestFileExt, tt.source, 0)
			if err != nil {
				t.Fatalf("%s %v", ErrMsgParseFailed, err)
			}

			detector := NewPointerDetector(fset, &bytes.Buffer{})
			detector.InspectWithParents(f)

			gotPointer := 0
			gotDeref := 0
			gotAddr := 0

			for _, u := range detector.usages {
				switch u.Kind {
				case "PointerType":
					gotPointer++
				case "Deref":
					gotDeref++
				case "AddressOf":
					gotAddr++
				}
			}

			if gotPointer != tt.wantPointer {
				t.Errorf(ErrMsgUnexpectedPointerCount, gotPointer, tt.wantPointer)
			}
			if gotDeref != tt.wantDeref {
				t.Errorf(ErrMsgUnexpectedDerefCount, gotDeref, tt.wantDeref)
			}
			if gotAddr != tt.wantAddressOf {
				t.Errorf(ErrMsgUnexpectedAddrCount, gotAddr, tt.wantAddressOf)
			}
		})
	}
}

func TestParentTraversal_NestedTypeContext(t *testing.T) {
	source := `
package p

type Config struct {
    handlers []func() *Result
    cache    map[string]*Entry
}

func New() *Config {
    return &Config{}
}
`

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, TestFilePrefix+TestNameNestedTypeContext+TestFileExt, source, 0)
	if err != nil {
		t.Fatal(err)
	}

	detector := NewPointerDetector(fset, &bytes.Buffer{})
	detector.InspectWithParents(f)

	pointerCount := 0
	for _, u := range detector.usages {
		if u.Kind == "PointerType" {
			pointerCount++
		}
	}

	if pointerCount != WantPointerCountNested {
		t.Errorf(ErrMsgUnexpectedPointerTotal, WantPointerCountNested, pointerCount)
	}
}

func TestParentTraversal_NoFalseDerefInTypeDecl(t *testing.T) {
	source := `
package p

type Wrapper struct {
    value *int
}

var global *Wrapper
`

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, TestFilePrefix+TestNameNoFalseDerefInTypeDecl2+TestFileExt, source, 0)
	if err != nil {
		t.Fatal(err)
	}

	detector := NewPointerDetector(fset, &bytes.Buffer{})
	detector.InspectWithParents(f)

	for _, u := range detector.usages {
		if u.Kind == "Deref" {
			t.Errorf(ErrMsgFalseDerefDetected, u)
		}
	}

	pointerCount := 0
	for _, u := range detector.usages {
		if u.Kind == "PointerType" {
			pointerCount++
		}
	}
	if pointerCount != WantPointerCountGlobalWrapper {
		t.Errorf(ErrMsgUnexpectedPointerCount, pointerCount, WantPointerCountGlobalWrapper)
	}
}

func TestNewPointerDetector_nilWriter(t *testing.T) {
	fset := token.NewFileSet()
	detector := NewPointerDetector(fset, nil)

	if detector.writer == nil {
		t.Error(ErrMsgWriterNil)
	}
}
