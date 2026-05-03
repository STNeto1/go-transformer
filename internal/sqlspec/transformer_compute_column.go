package sqlspec

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/parser"
)

type ComputeColumn struct {
	Name    string
	SQLType string
	Expr    string
}

var computeTemplateTokenRe = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func DeriveComputeColumns(base TableSpec, derivedTableName string, computes []ComputeColumn) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(computes) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one compute column is required")
	}

	baseColsByLower := map[string]ColumnSpec{}
	for _, c := range base.Columns {
		baseColsByLower[strings.ToLower(c.Name)] = c
	}

	computedCols := make([]ColumnSpec, 0, len(computes))
	computedExprs := make([]ast.Expression, 0, len(computes))
	computedTargetLower := map[string]struct{}{}

	for _, compute := range computes {
		if compute.Name == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("compute column name is required")
		}
		if compute.SQLType == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("compute column SQL type is required for %q", compute.Name)
		}
		if strings.TrimSpace(compute.Expr) == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("compute expression is required for %q", compute.Name)
		}

		targetLower := strings.ToLower(compute.Name)
		if _, exists := computedTargetLower[targetLower]; exists {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate compute target %q", compute.Name)
		}
		if _, exists := baseColsByLower[targetLower]; exists {
			return ResolvedBranch{}, nil, fmt.Errorf("compute target %q collides with existing column", compute.Name)
		}
		computedTargetLower[targetLower] = struct{}{}

		var expr ast.Expression
		var err error
		if strings.Contains(compute.Expr, "{{") || strings.Contains(compute.Expr, "}}") {
			expr, err = buildTemplateComputeExpr(baseColsByLower, compute)
		} else {
			expr, err = buildRawComputeExpr(baseColsByLower, computedTargetLower, compute)
		}
		if err != nil {
			return ResolvedBranch{}, nil, err
		}

		computedCols = append(computedCols, ColumnSpec{Name: compute.Name, SQLType: compute.SQLType})
		computedExprs = append(computedExprs, expr)
	}

	resolvedCols := make([]ColumnSpec, len(base.Columns), len(base.Columns)+len(computedCols))
	copy(resolvedCols, base.Columns)
	resolvedCols = append(resolvedCols, computedCols...)

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: resolvedCols,
		},
	}

	selectFn := func(selectAst *ast.SelectStatement) {
		selectCols := make([]ast.Expression, 0, len(base.Columns)+len(computedExprs))
		for _, c := range base.Columns {
			selectCols = append(selectCols, &ast.Identifier{Name: c.Name})
		}
		selectCols = append(selectCols, computedExprs...)
		selectAst.Columns = selectCols
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}

func buildTemplateComputeExpr(baseColsByLower map[string]ColumnSpec, compute ComputeColumn) (ast.Expression, error) {
	matches := computeTemplateTokenRe.FindAllStringSubmatchIndex(compute.Expr, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("template expression for %q has no valid placeholders", compute.Name)
	}

	args := make([]ast.Expression, 0, len(matches)*2+1)
	last := 0
	for _, m := range matches {
		start := m[0]
		end := m[1]
		nameStart := m[2]
		nameEnd := m[3]

		if start > last {
			literal := compute.Expr[last:start]
			if literal != "" {
				args = append(args, &ast.LiteralValue{Value: literal, Type: "STRING"})
			}
		}

		placeholder := compute.Expr[nameStart:nameEnd]
		baseCol, ok := baseColsByLower[strings.ToLower(placeholder)]
		if !ok {
			return nil, fmt.Errorf("compute expression for %q references unknown column %q", compute.Name, placeholder)
		}
		args = append(args, &ast.Identifier{Name: baseCol.Name})
		last = end
	}

	if last < len(compute.Expr) {
		tail := compute.Expr[last:]
		if tail != "" {
			args = append(args, &ast.LiteralValue{Value: tail, Type: "STRING"})
		}
	}

	if len(args) == 1 {
		return args[0], nil
	}

	return &ast.FunctionCall{Name: "CONCAT", Arguments: args}, nil
}

func buildRawComputeExpr(baseColsByLower map[string]ColumnSpec, computedTargetLower map[string]struct{}, compute ComputeColumn) (ast.Expression, error) {
	parsed, err := parser.ParseBytes([]byte("SELECT " + compute.Expr + " FROM _compute_src"))
	if err != nil {
		return nil, fmt.Errorf("invalid compute expression for %q: %w", compute.Name, err)
	}
	if len(parsed.Statements) != 1 {
		return nil, fmt.Errorf("invalid compute expression for %q", compute.Name)
	}

	selectStmt, ok := parsed.Statements[0].(*ast.SelectStatement)
	if !ok {
		return nil, fmt.Errorf("invalid compute expression for %q", compute.Name)
	}
	if len(selectStmt.Columns) != 1 {
		return nil, fmt.Errorf("compute expression for %q must produce a single expression", compute.Name)
	}

	expr := selectStmt.Columns[0]
	var validationErr error
	ast.Inspect(expr, func(n ast.Node) bool {
		if validationErr != nil {
			return false
		}
		id, isIdentifier := n.(*ast.Identifier)
		if !isIdentifier {
			return true
		}
		if id.Name == "" {
			return true
		}
		if _, isComputed := computedTargetLower[strings.ToLower(id.Name)]; isComputed {
			validationErr = fmt.Errorf("compute expression for %q cannot reference computed column %q in the same node", compute.Name, id.Name)
			return false
		}
		baseCol, exists := baseColsByLower[strings.ToLower(id.Name)]
		if !exists {
			validationErr = fmt.Errorf("compute expression for %q references unknown column %q", compute.Name, id.Name)
			return false
		}
		id.Name = baseCol.Name
		return true
	})
	if validationErr != nil {
		return nil, validationErr
	}

	return expr, nil
}
