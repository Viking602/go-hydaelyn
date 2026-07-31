package venat

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPublicAnyContract(t *testing.T) {
	files, err := publicSurfaceFiles()
	if err != nil {
		t.Fatalf("publicSurfaceFiles() error = %v", err)
	}
	fset := token.NewFileSet()
	var violations []string
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("ParseFile(%q) error = %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch decl := node.(type) {
			case *ast.FuncDecl:
				violations = append(violations, anyReturnViolations(fset, path, decl)...)
				return false
			case *ast.GenDecl:
				violations = append(violations, anyFieldViolations(fset, path, decl)...)
				return false
			}
			return true
		})
	}
	if len(violations) > 0 {
		t.Fatalf("public any contract violations:\n%s", strings.Join(violations, "\n"))
	}
}

func publicSurfaceFiles() ([]string, error) {
	var files []string
	for _, pattern := range []string{"*.go", "api/*.go", "agent/*.go", "multiagent/*.go", "skill/*.go"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			files = append(files, path)
		}
	}
	return files, nil
}

func anyReturnViolations(fset *token.FileSet, path string, decl *ast.FuncDecl) []string {
	if !decl.Name.IsExported() && decl.Recv == nil {
		return nil
	}
	if decl.Type.Results == nil {
		return nil
	}
	if allowsAny(decl.Doc, nil) {
		return nil
	}
	var violations []string
	for _, result := range decl.Type.Results.List {
		if isAnySlice(result.Type) {
			violations = append(violations, formatNode(fset, path, result, "exported function returns []any"))
		}
	}
	return violations
}

func anyFieldViolations(fset *token.FileSet, path string, decl *ast.GenDecl) []string {
	if decl.Tok != token.TYPE {
		return nil
	}
	var violations []string
	for _, spec := range decl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok || !typeSpec.Name.IsExported() {
			continue
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			continue
		}
		for _, field := range structType.Fields.List {
			if !containsAny(field.Type) || allowsAny(field.Doc, field.Comment) {
				continue
			}
			for _, name := range field.Names {
				if name.IsExported() {
					violations = append(violations, formatNode(fset, path, field, "exported field containing any needs // godoc-allow-any"))
				}
			}
		}
	}
	return violations
}

func containsAny(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name == "any"
	case *ast.ArrayType:
		return containsAny(v.Elt)
	case *ast.MapType:
		return containsAny(v.Key) || containsAny(v.Value)
	case *ast.StarExpr:
		return containsAny(v.X)
	case *ast.SelectorExpr:
		return containsAny(v.X)
	case *ast.IndexExpr:
		return containsAny(v.X) || containsAny(v.Index)
	case *ast.IndexListExpr:
		if containsAny(v.X) {
			return true
		}
		for _, index := range v.Indices {
			if containsAny(index) {
				return true
			}
		}
	case *ast.InterfaceType:
		return len(v.Methods.List) == 0
	}
	return false
}

func isAnySlice(expr ast.Expr) bool {
	arrayType, ok := expr.(*ast.ArrayType)
	return ok && containsAny(arrayType.Elt)
}

func allowsAny(doc, comment *ast.CommentGroup) bool {
	return commentGroupContains(doc, "godoc-allow-any") ||
		commentGroupContains(comment, "godoc-allow-any") ||
		commentGroupContains(doc, "venat:allow-public-any") ||
		commentGroupContains(comment, "venat:allow-public-any")
}

func commentGroupContains(group *ast.CommentGroup, marker string) bool {
	if group == nil {
		return false
	}
	return strings.Contains(group.Text(), marker)
}

func formatNode(fset *token.FileSet, path string, node ast.Node, message string) string {
	pos := fset.Position(node.Pos())
	return path + ":" + strconv.Itoa(pos.Line) + ": " + message
}
