package main

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "metricslint",
	Doc:  "reports calls to panic and process-terminating functions in forbidden contexts",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Body == nil {
					continue
				}
				inspectNode(pass, decl.Body, isMainMain(pass, decl))
			case *ast.GenDecl:
				inspectGenDecl(pass, decl)
			}
		}
	}

	return nil, nil
}

func inspectGenDecl(pass *analysis.Pass, decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		for _, value := range valueSpec.Values {
			inspectNode(pass, value, false)
		}
	}
}

func inspectNode(pass *analysis.Pass, node ast.Node, allowExit bool) {
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			inspectNode(pass, node.Body, false)
			return false
		case *ast.CallExpr:
			if isBuiltinPanic(pass, node.Fun) {
				pass.Reportf(node.Fun.Pos(), "panic call is forbidden")
				return true
			}

			if allowExit {
				return true
			}

			if name, ok := forbiddenExitCall(pass, node.Fun); ok {
				pass.Reportf(node.Fun.Pos(), "%s must only be called from main.main", name)
			}
		}

		return true
	})
}

func isMainMain(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	return pass.Pkg.Name() == "main" && fn.Recv == nil && fn.Name.Name == "main"
}

func isBuiltinPanic(pass *analysis.Pass, fun ast.Expr) bool {
	ident, ok := fun.(*ast.Ident)
	if !ok {
		return false
	}

	builtin, ok := pass.TypesInfo.Uses[ident].(*types.Builtin)
	return ok && builtin.Name() == "panic"
}

func forbiddenExitCall(pass *analysis.Pass, fun ast.Expr) (string, bool) {
	switch fun := fun.(type) {
	case *ast.Ident:
		return forbiddenImportedFunction(pass, fun)
	case *ast.SelectorExpr:
		return forbiddenPackageSelector(pass, fun)
	default:
		return "", false
	}
}

func forbiddenImportedFunction(pass *analysis.Pass, ident *ast.Ident) (string, bool) {
	fn, ok := pass.TypesInfo.Uses[ident].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return "", false
	}

	switch fn.Pkg().Path() {
	case "os":
		if fn.Name() == "Exit" {
			return "os.Exit", true
		}
	case "log":
		if fn.Name() == "Fatal" {
			return "log.Fatal", true
		}
	}

	return "", false
}

func forbiddenPackageSelector(pass *analysis.Pass, selector *ast.SelectorExpr) (string, bool) {
	pkgIdent, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}

	pkgName, ok := pass.TypesInfo.Uses[pkgIdent].(*types.PkgName)
	if !ok {
		return "", false
	}

	switch pkgName.Imported().Path() {
	case "os":
		if selector.Sel.Name == "Exit" {
			return "os.Exit", true
		}
	case "log":
		if selector.Sel.Name == "Fatal" {
			return "log.Fatal", true
		}
	}

	return "", false
}
