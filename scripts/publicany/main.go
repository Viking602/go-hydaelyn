package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var requiredScopes = []string{
	"message",
	"provider",
	"tool",
	"skill",
	"agent",
	"orchestration",
	"durable",
}

type violation struct {
	path    string
	line    int
	message string
}

func main() {
	var violations []violation
	for _, scope := range requiredScopes {
		current, err := scanScope(scope)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL [public-any]: %v\n", err)
			os.Exit(1)
		}
		violations = append(violations, current...)
	}
	sort.Slice(violations, func(left, right int) bool {
		if violations[left].path != violations[right].path {
			return violations[left].path < violations[right].path
		}
		return violations[left].line < violations[right].line
	})
	if len(violations) > 0 {
		for _, current := range violations {
			fmt.Fprintf(os.Stderr, "%s:%d: %s\n", current.path, current.line, current.message)
		}
		fmt.Fprintf(os.Stderr, "FAIL: %d public any contract violation(s).\n", len(violations))
		os.Exit(1)
	}
	fmt.Println("OK: public any contract preserved.")
}

func scanScope(scope string) ([]violation, error) {
	info, err := os.Stat(scope)
	if err != nil {
		return nil, fmt.Errorf("required scope %q is missing: %w", scope, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("required scope %q is not a directory", scope)
	}

	rootProductionFiles := 0
	var violations []violation
	err = filepath.WalkDir(scope, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Dir(path) == scope {
			rootProductionFiles++
		}
		current, err := scanFile(path)
		if err != nil {
			return err
		}
		violations = append(violations, current...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if rootProductionFiles == 0 {
		return nil, fmt.Errorf("required scope %q has no production Go files", scope)
	}
	return violations, nil
}

func scanFile(path string) ([]violation, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, source, parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	lines := bytes.Split(source, []byte("\n"))
	violations := scanExportedFunctions(path, fileSet, file, lines)
	violations = append(violations, scanExportedStructs(path, fileSet, file, lines)...)
	return violations, nil
}

func scanExportedFunctions(path string, fileSet *token.FileSet, file *ast.File, lines [][]byte) []violation {
	var violations []violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !function.Name.IsExported() || function.Type.Results == nil {
			continue
		}
		if !fieldListContainsSliceAny(function.Type.Results) {
			continue
		}
		line := fileSet.Position(function.Pos()).Line
		if hasImmediateTag(lines, line, "//venat:allow-public-any") {
			continue
		}
		violations = append(violations, violation{path: path, line: line, message: "exported function returns []any without //venat:allow-public-any"})
	}
	return violations
}

func scanExportedStructs(path string, fileSet *token.FileSet, file *ast.File, lines [][]byte) []violation {
	var violations []violation
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || !typeSpec.Name.IsExported() {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			violations = append(violations, scanExportedFields(path, fileSet, structure, lines)...)
		}
	}
	return violations
}

func scanExportedFields(path string, fileSet *token.FileSet, structure *ast.StructType, lines [][]byte) []violation {
	var violations []violation
	for _, field := range structure.Fields.List {
		if !containsAny(field.Type) || !hasExportedFieldName(field) {
			continue
		}
		line := fileSet.Position(field.Pos()).Line
		if hasImmediateTag(lines, line, "// godoc-allow-any") {
			continue
		}
		violations = append(violations, violation{path: path, line: line, message: "exported field contains any without // godoc-allow-any"})
	}
	return violations
}

func hasExportedFieldName(field *ast.Field) bool {
	for _, name := range field.Names {
		if name.IsExported() {
			return true
		}
	}
	return false
}

func fieldListContainsSliceAny(fields *ast.FieldList) bool {
	for _, field := range fields.List {
		found := false
		ast.Inspect(field.Type, func(node ast.Node) bool {
			array, ok := node.(*ast.ArrayType)
			if !ok || array.Len != nil {
				return true
			}
			identifier, ok := array.Elt.(*ast.Ident)
			if ok && identifier.Name == "any" {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

func containsAny(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && identifier.Name == "any" {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasImmediateTag(lines [][]byte, declarationLine int, tag string) bool {
	previous := declarationLine - 2
	return previous >= 0 && previous < len(lines) && strings.Contains(string(lines[previous]), tag)
}
