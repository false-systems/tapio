// Implementation Completeness Checker for Tapio
import "github.com/yairfalse/tapio/tools/lib"
// Enforces Claude.md Rule A4: NO STUBS, NO SHORTCUTS, NO PLACEHOLDER CODE
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)


type ImplementationViolation struct {
	File     string
	Line     int
	Function string
	Issue    string
	Severity string
	Code     string
}

type CompletenessChecker struct {
	violations []ImplementationViolation
	fileSet    *token.FileSet

	// Forbidden patterns
	forbiddenPatterns []*regexp.Regexp
	todoPatterns      []*regexp.Regexp
	stubPatterns      []*regexp.Regexp
}

func main() {
	fmt.Printf("%s🚨 Tapio Implementation Completeness Check%s\n", lib.BOLD+lib.RED, lib.NC)
	fmt.Printf("Enforcing: NO STUBS, NO SHORTCUTS, NO PLACEHOLDER CODE\n\n")

	checker := &CompletenessChecker{
		violations: []ImplementationViolation{},
		fileSet:    token.NewFileSet(),
	}

	// Initialize forbidden patterns
	checker.initializePatterns()

	// Check all Go files
	err := filepath.Walk(".", checker.walkFunc)
	if err != nil {
		fmt.Printf("%sError walking directory: %v%s\n", lib.RED, err, lib.NC)
		os.Exit(1)
	}

	// Report results
	checker.reportResults()

	// Exit with error if violations found
	if len(checker.violations) > 0 {
		os.Exit(1)
	}
}

func (cc *CompletenessChecker) initializePatterns() {
	// Forbidden patterns (from Claude.md examples)
	forbiddenPatterns := []string{
		`fmt\.Errorf\("not implemented"\)`,
		`fmt\.Errorf\("not implemented.*"\)`,
		`errors\.New\("not implemented"\)`,
		`errors\.New\("not implemented.*"\)`,
		`return nil, fmt\.Errorf\("not implemented"\)`,
		`panic\("not implemented"\)`,
		`panic\("TODO.*"\)`,
	}

	// TODO patterns
	todoPatterns := []string{
		`// TODO:.*implement.*later`,
		`// TODO.*implement.*`,
		`// We'll add.*later`,
		`// We'll fix.*later`,
		`// FIXME.*later`,
		`// TODO.*FIXME.*`,
		`//.*placeholder.*implementation`,
		`//.*temporary.*implementation`,
	}

	// Stub patterns
	stubPatterns := []string{
		`func.*\{[\s]*return nil[\s]*\}`, // Empty return nil
		`func.*\{[\s]*return.*nil, fmt\.Errorf\("not implemented"\)[\s]*\}`,
		`func.*\{[\s]*// TODO.*[\s]*return`,
		`func.*\{[\s]*panic\("not implemented"\)[\s]*\}`,
	}

	// Compile patterns
	cc.forbiddenPatterns = make([]*regexp.Regexp, len(forbiddenPatterns))
	for i, pattern := range forbiddenPatterns {
		cc.forbiddenPatterns[i] = regexp.MustCompile(pattern)
	}

	cc.todoPatterns = make([]*regexp.Regexp, len(todoPatterns))
	for i, pattern := range todoPatterns {
		cc.todoPatterns[i] = regexp.MustCompile(pattern)
	}

	cc.stubPatterns = make([]*regexp.Regexp, len(stubPatterns))
	for i, pattern := range stubPatterns {
		cc.stubPatterns[i] = regexp.MustCompile(pattern)
	}
}

func (cc *CompletenessChecker) walkFunc(path string, info os.FileInfo, err error) error {
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

	return cc.checkFile(path)
}

func (cc *CompletenessChecker) checkFile(filePath string) error {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Check patterns line by line
	cc.checkPatterns(filePath, string(content))

	// Parse AST for deeper analysis
	file, err := parser.ParseFile(cc.fileSet, filePath, content, parser.ParseComments)
	if err != nil {
		// If we can't parse, still check patterns
		return nil
	}

	// Check AST for stub functions
	ast.Inspect(file, func(n ast.Node) bool {
		return cc.checkASTNode(filePath, n)
	})

	return nil
}

func (cc *CompletenessChecker) checkPatterns(filePath, content string) {
	lines := strings.Split(content, "\n")

	for lineNum, line := range lines {
		lineNum++ // 1-indexed

		// Check forbidden patterns
		for _, pattern := range cc.forbiddenPatterns {
			if pattern.MatchString(line) {
				cc.addViolation(ImplementationViolation{
					File:     filePath,
					Line:     lineNum,
					Issue:    "FORBIDDEN: Function returns 'not implemented' error",
					Severity: "CRITICAL",
					Code:     strings.TrimSpace(line),
				})
			}
		}

		// Check TODO patterns
		for _, pattern := range cc.todoPatterns {
			if pattern.MatchString(line) {
				cc.addViolation(ImplementationViolation{
					File:     filePath,
					Line:     lineNum,
					Issue:    "FORBIDDEN: TODO comment indicating incomplete implementation",
					Severity: "CRITICAL",
					Code:     strings.TrimSpace(line),
				})
			}
		}

		// Check stub patterns
		for _, pattern := range cc.stubPatterns {
			if pattern.MatchString(line) {
				cc.addViolation(ImplementationViolation{
					File:     filePath,
					Line:     lineNum,
					Issue:    "FORBIDDEN: Stub function with empty or placeholder implementation",
					Severity: "CRITICAL",
					Code:     strings.TrimSpace(line),
				})
			}
		}
	}
}

func (cc *CompletenessChecker) checkASTNode(filePath string, n ast.Node) bool {
	switch node := n.(type) {
	case *ast.FuncDecl:
		cc.checkFunction(filePath, node)
	}
	return true
}

func (cc *CompletenessChecker) checkFunction(filePath string, fn *ast.FuncDecl) {
	if fn.Body == nil {
		return // Interface method or external function
	}

	funcName := fn.Name.Name
	pos := cc.fileSet.Position(fn.Pos())

	// Check for empty function body
	if len(fn.Body.List) == 0 {
		cc.addViolation(ImplementationViolation{
			File:     filePath,
			Line:     pos.Line,
			Function: funcName,
			Issue:    "FORBIDDEN: Empty function body",
			Severity: "CRITICAL",
			Code:     "func " + funcName + "() { /* empty */ }",
		})
		return
	}

	// Check for single return nil
	if len(fn.Body.List) == 1 {
		if ret, ok := fn.Body.List[0].(*ast.ReturnStmt); ok {
			if len(ret.Results) == 1 {
				if ident, ok := ret.Results[0].(*ast.Ident); ok && ident.Name == "nil" {
					cc.addViolation(ImplementationViolation{
						File:     filePath,
						Line:     pos.Line,
						Function: funcName,
						Issue:    "FORBIDDEN: Function only returns nil (likely stub)",
						Severity: "CRITICAL",
						Code:     "return nil",
					})
				}
			}
		}
	}

	// Check for panic with not implemented
	for _, stmt := range fn.Body.List {
		if expr, ok := stmt.(*ast.ExprStmt); ok {
			if call, ok := expr.X.(*ast.CallExpr); ok {
				if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
					if len(call.Args) > 0 {
						if lit, ok := call.Args[0].(*ast.BasicLit); ok {
							if strings.Contains(lit.Value, "not implemented") {
								cc.addViolation(ImplementationViolation{
									File:     filePath,
									Line:     cc.fileSet.Position(stmt.Pos()).Line,
									Function: funcName,
									Issue:    "FORBIDDEN: Panic with 'not implemented'",
									Severity: "CRITICAL",
									Code:     lit.Value,
								})
							}
						}
					}
				}
			}
		}
	}
}

func (cc *CompletenessChecker) addViolation(violation ImplementationViolation) {
	cc.violations = append(cc.violations, violation)
}

func (cc *CompletenessChecker) reportResults() {
	if len(cc.violations) == 0 {
		fmt.Printf("%s✅ Implementation completeness PASSED%s\n", lib.GREEN+lib.BOLD, lib.NC)
		fmt.Printf("No stubs, shortcuts, or placeholder code found\n")
		fmt.Printf("All functions are fully implemented according to Claude.md Rule A4\n")
		return
	}

	fmt.Printf("%s❌ Implementation completeness FAILED%s\n", lib.RED+lib.BOLD, lib.NC)
	fmt.Printf("Found %d implementation violations:\n\n", len(cc.violations))

	// Group by severity
	critical := []ImplementationViolation{}
	for _, v := range cc.violations {
		if v.Severity == "CRITICAL" {
			critical = append(critical, v)
		}
	}

	// Report critical violations
	fmt.Printf("%s🚨 CRITICAL VIOLATIONS (Build-blocking):%s\n", lib.RED+lib.BOLD, lib.NC)
	for i, v := range critical {
		fmt.Printf("%s%d. %s%s\n", lib.RED, i+1, v.Issue, lib.NC)
		fmt.Printf("   File: %s:%d\n", v.File, v.Line)
		if v.Function != "" {
			fmt.Printf("   Function: %s\n", v.Function)
		}
		fmt.Printf("   Code: %s\n", v.Code)
		fmt.Printf("\n")
	}

	fmt.Printf("%sClaude.md Rule A4 - Implementation Completeness:%s\n", lib.BLUE+lib.BOLD, lib.NC)
	fmt.Printf("❌ FORBIDDEN: Functions that return 'not implemented'\n")
	fmt.Printf("❌ FORBIDDEN: Empty function bodies with TODO comments\n")
	fmt.Printf("❌ FORBIDDEN: Placeholder implementations that don't work\n")
	fmt.Printf("❌ FORBIDDEN: 'We'll fix this later' code\n")
	fmt.Printf("✅ REQUIlib.RED: Every function must be fully implemented and working\n")
	fmt.Printf("✅ REQUIlib.RED: Every feature must be complete before moving to next component\n")
	fmt.Printf("✅ REQUIlib.RED: All code paths must be tested and functional\n")

	fmt.Printf("\n%sTo fix these violations:%s\n", lib.YELLOW+lib.BOLD, lib.NC)
	fmt.Printf("1. Replace all stub functions with real implementations\n")
	fmt.Printf("2. Remove all TODO comments indicating incomplete code\n")
	fmt.Printf("3. Implement proper error handling instead of 'not implemented'\n")
	fmt.Printf("4. Add comprehensive logic to all function bodies\n")
	fmt.Printf("5. Test all code paths to ensure they work correctly\n")

	fmt.Printf("\n%sExample of REQUIlib.RED pattern:%s\n", lib.GREEN+lib.BOLD, lib.NC)
	fmt.Printf(`func (c *Collector) CollectEvents(ctx context.Context, criteria Criteria) ([]Event, error) {
    if err := criteria.Validate(); err != nil {
        return nil, fmt.Errorf("invalid criteria: %%w", err)
    }
    
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    
    // Real implementation with error handling, validation, and proper logic
    events, err := c.queryDataSource(ctx, criteria)
    if err != nil {
        return nil, fmt.Errorf("failed to query data source: %%w", err)
    }
    
    return events, nil
}`)
}
