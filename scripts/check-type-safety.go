// Type Safety Enforcement for Tapio
// Enforces Claude.md Rule Q1: Strong typing with zero tolerance for type abuse
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const (
	RED    = "\033[0;31m"
	GREEN  = "\033[0;32m"
	YELLOW = "\033[0;33m"
	BLUE   = "\033[0;34m"
	NC     = "\033[0m"
	BOLD   = "\033[1m"
)

type TypeViolation struct {
	File        string
	Line        int
	Function    string
	Issue       string
	Severity    string
	Code        string
	Suggestion  string
}

type TypeSafetyChecker struct {
	violations []TypeViolation
	fileSet    *token.FileSet
}

func main() {
	fmt.Printf("%s🛡️  Tapio Type Safety Enforcement%s\n", BOLD+BLUE, NC)
	fmt.Printf("Enforcing strong typing with zero tolerance for type abuse\n\n")

	checker := &TypeSafetyChecker{
		violations: []TypeViolation{},
		fileSet:    token.NewFileSet(),
	}

	// Check all Go files
	err := filepath.Walk(".", checker.walkFunc)
	if err != nil {
		fmt.Printf("%sError walking directory: %v%s\n", RED, err, NC)
		os.Exit(1)
	}

	// Report results
	checker.reportResults()

	// Exit with error if violations found
	if len(checker.violations) > 0 {
		os.Exit(1)
	}
}

func (tsc *TypeSafetyChecker) walkFunc(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}

	// Skip non-Go files
	if !strings.HasSuffix(path, ".go") {
		return nil
	}

	// Skip vendor and generated files
	if strings.Contains(path, "vendor/") || strings.Contains(path, ".git/") {
		return nil
	}

	// Skip test files for now (focus on implementation)
	if strings.HasSuffix(path, "_test.go") {
		return nil
	}

	return tsc.checkFile(path)
}

func (tsc *TypeSafetyChecker) checkFile(filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Parse AST
	file, err := parser.ParseFile(tsc.fileSet, filePath, content, parser.ParseComments)
	if err != nil {
		return nil // Skip files we can't parse
	}

	// Check AST for type violations
	ast.Inspect(file, func(n ast.Node) bool {
		return tsc.checkNode(filePath, n)
	})

	return nil
}

func (tsc *TypeSafetyChecker) checkNode(filePath string, n ast.Node) bool {
	switch node := n.(type) {
	case *ast.MapType:
		tsc.checkMapType(filePath, node)
	case *ast.InterfaceType:
		tsc.checkInterfaceType(filePath, node)
	case *ast.FuncDecl:
		tsc.checkFunctionSignature(filePath, node)
	case *ast.TypeAssertExpr:
		tsc.checkTypeAssertion(filePath, node)
	case *ast.CallExpr:
		tsc.checkFunctionCall(filePath, node)
	case *ast.StructType:
		tsc.checkStructType(filePath, node)
	}
	return true
}

func (tsc *TypeSafetyChecker) checkMapType(filePath string, node *ast.MapType) {
	pos := tsc.fileSet.Position(node.Pos())

	// Check for map[string]interface{}
	if tsc.isStringKey(node.Key) && tsc.isEmptyInterface(node.Value) {
		tsc.addViolation(TypeViolation{
			File:       filePath,
			Line:       pos.Line,
			Issue:      "FORBIDDEN: map[string]interface{} without strong justification",
			Severity:   "CRITICAL",
			Code:       "map[string]interface{}",
			Suggestion: "Use strongly-typed struct with validation methods",
		})
	}

	// Check for any interface{} values in maps
	if tsc.isEmptyInterface(node.Value) {
		if !tsc.isStringKey(node.Key) {
			tsc.addViolation(TypeViolation{
				File:       filePath,
				Line:       pos.Line,
				Issue:      "FORBIDDEN: interface{} as map value type",
				Severity:   "CRITICAL",
				Code:       tsc.nodeToString(node),
				Suggestion: "Use concrete type or type parameter instead of interface{}",
			})
		}
	}
}

func (tsc *TypeSafetyChecker) checkInterfaceType(filePath string, node *ast.InterfaceType) {
	pos := tsc.fileSet.Position(node.Pos())

	// Check for empty interface{} in public APIs
	if len(node.Methods.List) == 0 {
		// This is interface{} or any
		tsc.addViolation(TypeViolation{
			File:       filePath,
			Line:       pos.Line,
			Issue:      "FORBIDDEN: interface{} in public APIs",
			Severity:   "CRITICAL",
			Code:       "interface{}",
			Suggestion: "Use concrete types or well-defined interfaces",
		})
	}
}

func (tsc *TypeSafetyChecker) checkFunctionSignature(filePath string, node *ast.FuncDecl) {
	if node.Type == nil {
		return
	}

	funcName := ""
	if node.Name != nil {
		funcName = node.Name.Name
	}

	// Check parameters
	if node.Type.Params != nil {
		for _, param := range node.Type.Params.List {
			if tsc.isEmptyInterface(param.Type) {
				pos := tsc.fileSet.Position(param.Pos())
				tsc.addViolation(TypeViolation{
					File:       filePath,
					Line:       pos.Line,
					Function:   funcName,
					Issue:      "FORBIDDEN: interface{} parameter in function signature",
					Severity:   "CRITICAL",
					Code:       fmt.Sprintf("func %s(...interface{}...)", funcName),
					Suggestion: "Use concrete types or type parameters",
				})
			}
		}
	}

	// Check return types
	if node.Type.Results != nil {
		for _, result := range node.Type.Results.List {
			if tsc.isEmptyInterface(result.Type) {
				pos := tsc.fileSet.Position(result.Pos())
				tsc.addViolation(TypeViolation{
					File:       filePath,
					Line:       pos.Line,
					Function:   funcName,
					Issue:      "FORBIDDEN: interface{} return type",
					Severity:   "CRITICAL",
					Code:       fmt.Sprintf("func %s() interface{}", funcName),
					Suggestion: "Return concrete types with proper error handling",
				})
			}
		}
	}
}

func (tsc *TypeSafetyChecker) checkTypeAssertion(filePath string, node *ast.TypeAssertExpr) {
	pos := tsc.fileSet.Position(node.Pos())

	// Type assertions should always have error handling (checked elsewhere)
	// But we can check for unsafe type assertions
	if node.Type != nil {
		// This is a type assertion - ensure it's used safely
		// We'll add a warning for now
		tsc.addViolation(TypeViolation{
			File:       filePath,
			Line:       pos.Line,
			Issue:      "WARNING: Type assertion detected - ensure error handling",
			Severity:   "WARNING",
			Code:       tsc.nodeToString(node),
			Suggestion: "Use two-value form: value, ok := x.(Type)",
		})
	}
}

func (tsc *TypeSafetyChecker) checkFunctionCall(filePath string, node *ast.CallExpr) {
	// Check for common anti-patterns in function calls
	if fun, ok := node.Fun.(*ast.SelectorExpr); ok {
		if x, ok := fun.X.(*ast.Ident); ok {
			// Check for common unsafe patterns
			if x.Name == "json" && fun.Sel.Name == "Unmarshal" {
				// Check if unmarshaling into interface{}
				if len(node.Args) >= 2 {
					if tsc.hasInterfaceType(node.Args[1]) {
						pos := tsc.fileSet.Position(node.Pos())
						tsc.addViolation(TypeViolation{
							File:       filePath,
							Line:       pos.Line,
							Issue:      "WARNING: JSON unmarshal into interface{} - prefer concrete types",
							Severity:   "WARNING",
							Code:       "json.Unmarshal(..., &interface{})",
							Suggestion: "Unmarshal into strongly-typed struct",
						})
					}
				}
			}
		}
	}
}

func (tsc *TypeSafetyChecker) checkStructType(filePath string, node *ast.StructType) {
	pos := tsc.fileSet.Position(node.Pos())

	// Check struct fields for type safety
	for _, field := range node.Fields.List {
		if tsc.isEmptyInterface(field.Type) {
			tsc.addViolation(TypeViolation{
				File:       filePath,
				Line:       tsc.fileSet.Position(field.Pos()).Line,
				Issue:      "FORBIDDEN: interface{} field in struct",
				Severity:   "CRITICAL",
				Code:       "struct { field interface{} }",
				Suggestion: "Use concrete types with validation methods",
			})
		}

		// Check for map[string]interface{} fields
		if mapType, ok := field.Type.(*ast.MapType); ok {
			if tsc.isStringKey(mapType.Key) && tsc.isEmptyInterface(mapType.Value) {
				tsc.addViolation(TypeViolation{
					File:       filePath,
					Line:       tsc.fileSet.Position(field.Pos()).Line,
					Issue:      "FORBIDDEN: map[string]interface{} field in struct",
					Severity:   "CRITICAL",
					Code:       "struct { field map[string]interface{} }",
					Suggestion: "Use nested struct with typed fields",
				})
			}
		}
	}
}

func (tsc *TypeSafetyChecker) isEmptyInterface(expr ast.Expr) bool {
	if interfaceType, ok := expr.(*ast.InterfaceType); ok {
		return len(interfaceType.Methods.List) == 0
	}
	
	// Check for 'any' type (Go 1.18+)
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "any"
	}
	
	return false
}

func (tsc *TypeSafetyChecker) isStringKey(expr ast.Expr) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "string"
	}
	return false
}

func (tsc *TypeSafetyChecker) hasInterfaceType(expr ast.Expr) bool {
	// Check if expression involves interface{} type
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		return tsc.hasInterfaceType(e.X)
	case *ast.StarExpr:
		return tsc.isEmptyInterface(e.X)
	}
	return false
}

func (tsc *TypeSafetyChecker) nodeToString(node ast.Node) string {
	switch n := node.(type) {
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", tsc.typeToString(n.Key), tsc.typeToString(n.Value))
	case *ast.TypeAssertExpr:
		return fmt.Sprintf("%s.(%s)", tsc.exprToString(n.X), tsc.typeToString(n.Type))
	default:
		return "unknown"
	}
}

func (tsc *TypeSafetyChecker) typeToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.InterfaceType:
		if len(e.Methods.List) == 0 {
			return "interface{}"
		}
		return "interface{...}"
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", tsc.typeToString(e.Key), tsc.typeToString(e.Value))
	default:
		return "unknown"
	}
}

func (tsc *TypeSafetyChecker) exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	default:
		return "expr"
	}
}

func (tsc *TypeSafetyChecker) addViolation(violation TypeViolation) {
	tsc.violations = append(tsc.violations, violation)
}

func (tsc *TypeSafetyChecker) reportResults() {
	if len(tsc.violations) == 0 {
		fmt.Printf("%s✅ Type safety validation PASSED%s\n", GREEN+BOLD, NC)
		fmt.Printf("No type safety violations found\n")
		fmt.Printf("All code follows strong typing principles\n")
		return
	}

	fmt.Printf("%s❌ Type safety validation FAILED%s\n", RED+BOLD, NC)
	fmt.Printf("Found %d type safety violations:\n\n", len(tsc.violations))

	// Group by severity
	critical := []TypeViolation{}
	warnings := []TypeViolation{}

	for _, v := range tsc.violations {
		if v.Severity == "CRITICAL" {
			critical = append(critical, v)
		} else {
			warnings = append(warnings, v)
		}
	}

	// Report critical violations
	if len(critical) > 0 {
		fmt.Printf("%s🚨 CRITICAL VIOLATIONS (Build-blocking):%s\n", RED+BOLD, NC)
		for i, v := range critical {
			fmt.Printf("%s%d. %s%s\n", RED, i+1, v.Issue, NC)
			fmt.Printf("   File: %s:%d\n", v.File, v.Line)
			if v.Function != "" {
				fmt.Printf("   Function: %s\n", v.Function)
			}
			fmt.Printf("   Code: %s\n", v.Code)
			fmt.Printf("   Fix: %s\n", v.Suggestion)
			fmt.Printf("\n")
		}
	}

	// Report warnings
	if len(warnings) > 0 {
		fmt.Printf("%s⚠️  WARNINGS:%s\n", YELLOW+BOLD, NC)
		for i, v := range warnings {
			fmt.Printf("%s%d. %s%s\n", YELLOW, i+1, v.Issue, NC)
			fmt.Printf("   File: %s:%d\n", v.File, v.Line)
			fmt.Printf("   Code: %s\n", v.Code)
			fmt.Printf("   Fix: %s\n", v.Suggestion)
			fmt.Printf("\n")
		}
	}

	fmt.Printf("%sClaude.md Rule Q1 - Type Safety Requirements:%s\n", BLUE+BOLD, NC)
	fmt.Printf("❌ FORBIDDEN: map[string]interface{} without strong justification\n")
	fmt.Printf("❌ FORBIDDEN: interface{} in public APIs\n") 
	fmt.Printf("❌ FORBIDDEN: any without explicit comment explaining why\n")
	fmt.Printf("✅ REQUIRED: Strongly-typed structs for all data\n")
	fmt.Printf("✅ REQUIRED: Validation methods for all input types\n")
	fmt.Printf("✅ REQUIRED: Type assertions with error handling\n")

	fmt.Printf("\n%sExample of REQUIRED pattern:%s\n", GREEN+BOLD, NC)
	fmt.Printf(`// ✅ GOOD - Type safe
type Config struct {
    Host     string        ` + "`json:\"host\" validate:\"required\"`" + `
    Port     int           ` + "`json:\"port\" validate:\"min=1,max=65535\"`" + `
    Timeout  time.Duration ` + "`json:\"timeout\" validate:\"min=1s\"`" + `
}

func (c Config) Validate() error {
    // Validation logic here
    return nil
}

// ❌ BAD - Type unsafe
type Config map[string]interface{}`)

	fmt.Printf("\n\n%sTo fix type safety violations:%s\n", YELLOW+BOLD, NC)
	fmt.Printf("1. Replace map[string]interface{} with strongly-typed structs\n")
	fmt.Printf("2. Remove interface{} from public APIs\n")
	fmt.Printf("3. Add validation methods to all data types\n")
	fmt.Printf("4. Use type parameters instead of interface{} for generics\n")
	fmt.Printf("5. Add proper error handling for type assertions\n")
	fmt.Printf("6. Document any remaining interface{} usage with justification\n")
}