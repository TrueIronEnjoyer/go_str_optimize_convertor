package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

const bufSize = 32

func collectNodesInForLoop(f *ast.File) map[ast.Node]bool {
	nodes := make(map[ast.Node]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		if forStmt, ok := n.(*ast.ForStmt); ok {
			ast.Inspect(forStmt.Body, func(n2 ast.Node) bool {
				if n2 != nil {
					nodes[n2] = true
				}
				return true
			})
		}
		return true
	})
	return nodes
}

func flattenConcat(expr ast.Expr) []ast.Expr {
	if bin, ok := expr.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		left := flattenConcat(bin.X)
		right := flattenConcat(bin.Y)
		return append(left, right...)
	}
	return []ast.Expr{expr}
}

func chunkString(s string) ast.Expr {
	unquoted := strings.Trim(s, "\"")
	chunks := splitChunks(unquoted, bufSize)
	var exprs []ast.Expr
	for _, chunk := range chunks {
		exprs = append(exprs, &ast.BasicLit{
			Kind:  token.STRING,
			Value: fmt.Sprintf("%q", chunk),
		})
	}
	return makeFlatBinaryExpr(exprs)
}

func makeFlatBinaryExpr(exprs []ast.Expr) ast.Expr {
	if len(exprs) == 0 {
		return nil
	}
	result := exprs[0]
	for _, expr := range exprs[1:] {
		result = &ast.BinaryExpr{
			X:  result,
			Op: token.ADD,
			Y:  expr,
		}
	}
	return result
}

func processConcatExpr(expr ast.Expr, first bool) ast.Expr {
	parts := flattenConcat(expr)
	var newParts []ast.Expr
	for i, part := range parts {
		if i == 0 && first {
			newParts = append(newParts, part)
		} else if basic, ok := part.(*ast.BasicLit); ok && basic.Kind == token.STRING {
			chunkedExpr := chunkString(basic.Value)
			chunkedParts := flattenConcat(chunkedExpr)
			newParts = append(newParts, chunkedParts...)
		} else {
			newParts = append(newParts, part)
		}
	}
	return makeFlatBinaryExpr(newParts)
}

func splitChunks(s string, size int) []string {
	var chunks []string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

func transformNonLoopAddAssign(f *ast.File, nodesInLoop map[ast.Node]bool) {
	astutil.Apply(f, func(cursor *astutil.Cursor) bool {
		node := cursor.Node()
		assign, ok := node.(*ast.AssignStmt)
		if !ok || assign.Tok != token.ADD_ASSIGN || len(assign.Lhs) != 1 {
			return true
		}
		ident, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		varName := ident.Name

		if nodesInLoop[node] {
			return true
		}

		builderName := varName + "Builder"
		newExpr := processConcatExpr(assign.Rhs[0], false)

		newBlock := &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.DeclStmt{
					Decl: &ast.GenDecl{
						Tok: token.VAR,
						Specs: []ast.Spec{
							&ast.ValueSpec{
								Names: []*ast.Ident{ast.NewIdent(builderName)},
								Type: &ast.SelectorExpr{
									X:   ast.NewIdent("strings"),
									Sel: ast.NewIdent("Builder"),
								},
							},
						},
					},
				},
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   ast.NewIdent(builderName),
							Sel: ast.NewIdent("WriteString"),
						},
						Args: []ast.Expr{ast.NewIdent(varName)},
					},
				},
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   ast.NewIdent(builderName),
							Sel: ast.NewIdent("WriteString"),
						},
						Args: []ast.Expr{newExpr},
					},
				},
				&ast.AssignStmt{
					Lhs: []ast.Expr{ast.NewIdent(varName)},
					Tok: token.ASSIGN,
					Rhs: []ast.Expr{
						&ast.CallExpr{
							Fun: &ast.SelectorExpr{
								X:   ast.NewIdent(builderName),
								Sel: ast.NewIdent("String"),
							},
						},
					},
				},
			},
		}
		cursor.Replace(newBlock)
		return false
	}, nil)
}

func transformForLoopConcatenation(f *ast.File) {
	astutil.Apply(f, func(cursor *astutil.Cursor) bool {
		forStmt, ok := cursor.Node().(*ast.ForStmt)
		if !ok {
			return true
		}

		var concatVars []string
		ast.Inspect(forStmt.Body, func(n ast.Node) bool {
			if assign, ok := n.(*ast.AssignStmt); ok && assign.Tok == token.ADD_ASSIGN && len(assign.Lhs) == 1 {
				if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
					concatVars = append(concatVars, ident.Name)
				}
			}
			return true
		})

		if len(concatVars) == 0 {
			return true
		}

		for _, varName := range concatVars {
			builderName := varName + "Builder"
			newBody := *forStmt.Body
			for i, stmt := range newBody.List {
				if assign, ok := stmt.(*ast.AssignStmt); ok && assign.Tok == token.ADD_ASSIGN {
					if ident, ok := assign.Lhs[0].(*ast.Ident); ok && ident.Name == varName {
						newExpr := processConcatExpr(assign.Rhs[0], false)
						newStmt := &ast.ExprStmt{
							X: &ast.CallExpr{
								Fun: &ast.SelectorExpr{
									X:   ast.NewIdent(builderName),
									Sel: ast.NewIdent("WriteString"),
								},
								Args: []ast.Expr{newExpr},
							},
						}
						newBody.List[i] = newStmt
					}
				}
			}

			declStmt := &ast.DeclStmt{
				Decl: &ast.GenDecl{
					Tok: token.VAR,
					Specs: []ast.Spec{
						&ast.ValueSpec{
							Names: []*ast.Ident{ast.NewIdent(builderName)},
							Type: &ast.SelectorExpr{
								X:   ast.NewIdent("strings"),
								Sel: ast.NewIdent("Builder"),
							},
						},
					},
				},
			}

			assignStmt := &ast.AssignStmt{
				Lhs: []ast.Expr{ast.NewIdent(varName)},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{
					&ast.BinaryExpr{
						X:  ast.NewIdent(varName),
						Op: token.ADD,
						Y: &ast.CallExpr{
							Fun: &ast.SelectorExpr{
								X:   ast.NewIdent(builderName),
								Sel: ast.NewIdent("String"),
							},
						},
					},
				},
			}

			newFor := &ast.ForStmt{
				For:  forStmt.For,
				Init: forStmt.Init,
				Cond: forStmt.Cond,
				Post: forStmt.Post,
				Body: &newBody,
			}

			newBlock := &ast.BlockStmt{
				List: []ast.Stmt{
					declStmt,
					newFor,
					assignStmt,
				},
			}
			cursor.Replace(newBlock)
		}
		return false
	}, nil)
}

func isPureStringConcat(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return false
		}
		return isPureStringConcat(e.X) && isPureStringConcat(e.Y)
	default:
		return false
	}
}

func transformBinaryConcat(f *ast.File) {
	astutil.Apply(f, func(cursor *astutil.Cursor) bool {
		node := cursor.Node()
		binExpr, ok := node.(*ast.BinaryExpr)
		if !ok || binExpr.Op != token.ADD {
			return true
		}
		if isPureStringConcat(binExpr) {
			newExpr := processConcatExpr(binExpr, true)
			cursor.Replace(newExpr)
			return false
		}
		return true
	}, nil)
}

func removeParens(f *ast.File) {
	astutil.Apply(f, func(cursor *astutil.Cursor) bool {
		if parenExpr, ok := cursor.Node().(*ast.ParenExpr); ok {
			cursor.Replace(parenExpr.X)
		}
		return true
	}, nil)
}

func ensureStringsImport(fs *token.FileSet, f *ast.File) {
	for _, imp := range f.Imports {
		if imp.Path.Value == "\"strings\"" {
			return
		}
	}

	var usesStrings bool
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "strings" {
				usesStrings = true
				return false
			}
		}
		return true
	})

	if usesStrings {
		astutil.AddImport(fs, f, "strings")
	}
}

func processFile(filename string) {
	fs := token.NewFileSet()
	src, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Printf("Error reading file %s: %v\n", filename, err)
		return
	}
	node, err := parser.ParseFile(fs, filename, src, parser.AllErrors)
	if err != nil {
		log.Printf("Error parsing file %s: %v\n", filename, err)
		return
	}

	var hasAddAssign bool
	ast.Inspect(node, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok && assign.Tok == token.ADD_ASSIGN {
			hasAddAssign = true
			return false
		}
		return true
	})

	if hasAddAssign {
		nodesInLoop := collectNodesInForLoop(node)
		transformNonLoopAddAssign(node, nodesInLoop)
		transformForLoopConcatenation(node)
	}

	transformBinaryConcat(node)

	removeParens(node)
	ensureStringsImport(fs, node)

	var buf bytes.Buffer
	config := &printer.Config{
		Mode:     printer.RawFormat,
		Tabwidth: 8,
	}
	if err := config.Fprint(&buf, fs, node); err != nil {
		log.Printf("Error printing AST: %v", err)
		return
	}

	if err := ioutil.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		log.Printf("Error writing file: %v", err)
	}
	fmt.Println("Processed:", filename)
}

func processPath(path string) {
	info, err := os.Stat(path)
	if err != nil {
		log.Printf("Error accessing path %s: %v\n", path, err)
		return
	}
	if info.IsDir() {
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(info.Name(), ".go") {
				processFile(p)
			}
			return nil
		})
	} else {
		processFile(path)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go [file.go | directory]")
		return
	}
	for _, arg := range os.Args[1:] {
		processPath(arg)
	}
}
