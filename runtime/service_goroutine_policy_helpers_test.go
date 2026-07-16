package gsr_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type serviceGoroutineViolation struct {
	receiver string
	method   string
	position token.Position
}

func (v serviceGoroutineViolation) String() string {
	return fmt.Sprintf("%s:%d: %s.%s starts a goroutine", filepath.ToSlash(v.position.Filename), v.position.Line, v.receiver, v.method)
}

func serviceGoroutineViolationsFromSource(filename, source string) ([]serviceGoroutineViolation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return findServiceGoroutineViolations(fset, []*ast.File{file}), nil
}

func scanServiceGoroutineViolations(root string) ([]serviceGoroutineViolation, error) {
	pathsByDirectory := make(map[string][]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == "vendor" || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		pathsByDirectory[filepath.Dir(path)] = append(pathsByDirectory[filepath.Dir(path)], path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	directories := make([]string, 0, len(pathsByDirectory))
	for directory := range pathsByDirectory {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	violations := make([]serviceGoroutineViolation, 0)
	for _, directory := range directories {
		paths := pathsByDirectory[directory]
		sort.Strings(paths)
		fset := token.NewFileSet()
		filesByPackage := make(map[string][]*ast.File)
		for _, path := range paths {
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			if ast.IsGenerated(file) {
				continue
			}
			filesByPackage[file.Name.Name] = append(filesByPackage[file.Name.Name], file)
		}
		packages := make([]string, 0, len(filesByPackage))
		for name := range filesByPackage {
			packages = append(packages, name)
		}
		sort.Strings(packages)
		for _, name := range packages {
			violations = append(violations, findServiceGoroutineViolations(fset, filesByPackage[name])...)
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		left, right := violations[i].position, violations[j].position
		if left.Filename != right.Filename {
			return left.Filename < right.Filename
		}
		return left.Offset < right.Offset
	})
	return violations, nil
}

func findServiceGoroutineViolations(fset *token.FileSet, files []*ast.File) []serviceGoroutineViolation {
	methodsByReceiver := make(map[string][]*ast.FuncDecl)
	lifecycleMethods := make(map[string]map[string]bool)
	for _, file := range files {
		for _, declaration := range file.Decls {
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Recv == nil || len(method.Recv.List) == 0 {
				continue
			}
			receiver := receiverName(method.Recv.List[0].Type)
			if receiver == "" {
				continue
			}
			methodsByReceiver[receiver] = append(methodsByReceiver[receiver], method)
			if isServiceLifecycleMethod(method) {
				if lifecycleMethods[receiver] == nil {
					lifecycleMethods[receiver] = make(map[string]bool)
				}
				lifecycleMethods[receiver][method.Name.Name] = true
			}
		}
	}

	violations := make([]serviceGoroutineViolation, 0)
	for receiver, methods := range methodsByReceiver {
		if len(lifecycleMethods[receiver]) != 4 {
			continue
		}
		for _, method := range methods {
			ast.Inspect(method.Body, func(node ast.Node) bool {
				statement, ok := node.(*ast.GoStmt)
				if ok {
					violations = append(violations, serviceGoroutineViolation{
						receiver: receiver,
						method:   method.Name.Name,
						position: fset.Position(statement.Go),
					})
				}
				return true
			})
		}
	}
	return violations
}

func isServiceLifecycleMethod(method *ast.FuncDecl) bool {
	parameters := fieldCount(method.Type.Params)
	results := fieldCount(method.Type.Results)
	switch method.Name.Name {
	case "Init":
		return parameters == 1 && results == 1
	case "Handle":
		return parameters == 2 && results == 1
	case "Stop":
		return parameters == 1 && results == 1
	case "Close":
		return parameters == 0 && results == 1
	default:
		return false
	}
}

func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++
			continue
		}
		count += len(field.Names)
	}
	return count
}

func receiverName(expression ast.Expr) string {
	switch receiver := expression.(type) {
	case *ast.Ident:
		return receiver.Name
	case *ast.StarExpr:
		return receiverName(receiver.X)
	case *ast.IndexExpr:
		return receiverName(receiver.X)
	case *ast.IndexListExpr:
		return receiverName(receiver.X)
	default:
		return ""
	}
}
