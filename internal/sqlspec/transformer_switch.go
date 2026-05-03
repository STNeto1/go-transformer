package sqlspec

import (
	"fmt"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type SwitchBranchSpec struct {
	Label     string
	TableName string
	Mode      ConditionalMode
	Rules     []FilterRow
}

type SwitchSpec struct {
	Branches         []SwitchBranchSpec
	DefaultLabel     string
	DefaultTableName string
}

type SwitchResult struct {
	Branches        map[string]ResolvedBranch
	Backfills       map[string]*ast.InsertStatement
	DefaultBranch   ResolvedBranch
	DefaultBackfill *ast.InsertStatement
}

func DeriveSwitch(base TableSpec, spec SwitchSpec) (SwitchResult, error) {
	if base.Name == "" {
		return SwitchResult{}, fmt.Errorf("base table name is required")
	}
	if len(base.Columns) == 0 {
		return SwitchResult{}, fmt.Errorf("base table requires columns")
	}
	if len(spec.Branches) == 0 {
		return SwitchResult{}, fmt.Errorf("at least one switch branch is required")
	}
	if spec.DefaultLabel == "" {
		return SwitchResult{}, fmt.Errorf("default label is required")
	}
	if spec.DefaultTableName == "" {
		return SwitchResult{}, fmt.Errorf("default table name is required")
	}

	labels := map[string]struct{}{}
	tables := map[string]struct{}{}
	branchPredicates := make([]ast.Expression, 0, len(spec.Branches))
	hasMatchAllBranch := false
	branches := map[string]ResolvedBranch{}
	backfills := map[string]*ast.InsertStatement{}

	for _, b := range spec.Branches {
		if b.Label == "" {
			return SwitchResult{}, fmt.Errorf("branch label is required")
		}
		if b.TableName == "" {
			return SwitchResult{}, fmt.Errorf("branch table name is required")
		}

		labelKey := strings.ToLower(b.Label)
		if _, exists := labels[labelKey]; exists {
			return SwitchResult{}, fmt.Errorf("duplicate branch label %q", b.Label)
		}
		labels[labelKey] = struct{}{}

		tableKey := strings.ToLower(b.TableName)
		if _, exists := tables[tableKey]; exists {
			return SwitchResult{}, fmt.Errorf("duplicate branch table name %q", b.TableName)
		}
		tables[tableKey] = struct{}{}

		mode := b.Mode
		if mode == "" {
			mode = ConditionalModeAll
		}
		if mode != ConditionalModeAll && mode != ConditionalModeAny {
			return SwitchResult{}, fmt.Errorf("unsupported branch mode %q", b.Mode)
		}

		cols := make([]ColumnSpec, len(base.Columns))
		copy(cols, base.Columns)
		branchResolved := ResolvedBranch{Table: TableSpec{Name: b.TableName, Columns: cols}}

		var predicate ast.Expression
		var err error
		if len(b.Rules) > 0 {
			predicate, err = buildConditionalPredicate(base.Columns, b.Rules, mode)
			if err != nil {
				return SwitchResult{}, err
			}
		} else {
			hasMatchAllBranch = true
		}
		if predicate != nil {
			branchPredicates = append(branchPredicates, predicate)
		}

		stmt, err := BuildBackfillInsert(base.Name, branchResolved, func(s *ast.SelectStatement) {
			s.Where = predicate
		})
		if err != nil {
			return SwitchResult{}, err
		}

		branches[b.Label] = branchResolved
		backfills[b.Label] = stmt
	}

	if _, collides := labels[strings.ToLower(spec.DefaultLabel)]; collides {
		return SwitchResult{}, fmt.Errorf("default label %q collides with branch label", spec.DefaultLabel)
	}
	if _, collides := tables[strings.ToLower(spec.DefaultTableName)]; collides {
		return SwitchResult{}, fmt.Errorf("default table name %q collides with branch table name", spec.DefaultTableName)
	}

	defaultCols := make([]ColumnSpec, len(base.Columns))
	copy(defaultCols, base.Columns)
	defaultBranch := ResolvedBranch{Table: TableSpec{Name: spec.DefaultTableName, Columns: defaultCols}}

	defaultStmt, err := BuildBackfillInsert(base.Name, defaultBranch, func(s *ast.SelectStatement) {
		if hasMatchAllBranch {
			s.Where = alwaysFalseExpr()
			return
		}
		s.Where = buildSwitchDefaultPredicate(branchPredicates)
	})
	if err != nil {
		return SwitchResult{}, err
	}

	return SwitchResult{
		Branches:        branches,
		Backfills:       backfills,
		DefaultBranch:   defaultBranch,
		DefaultBackfill: defaultStmt,
	}, nil
}

func buildSwitchDefaultPredicate(branchPredicates []ast.Expression) ast.Expression {
	if len(branchPredicates) == 0 {
		return nil
	}

	orExpr := branchPredicates[0]
	for i := 1; i < len(branchPredicates); i++ {
		orExpr = &ast.BinaryExpression{Left: orExpr, Operator: "OR", Right: branchPredicates[i]}
	}

	return &ast.UnaryExpression{Operator: ast.Not, Expr: orExpr}
}

func alwaysTrueExpr() ast.Expression {
	return &ast.BinaryExpression{
		Left:     &ast.LiteralValue{Value: 1, Type: "INTEGER"},
		Operator: "=",
		Right:    &ast.LiteralValue{Value: 1, Type: "INTEGER"},
	}
}

func alwaysFalseExpr() ast.Expression {
	return &ast.BinaryExpression{
		Left:     &ast.LiteralValue{Value: 1, Type: "INTEGER"},
		Operator: "=",
		Right:    &ast.LiteralValue{Value: 0, Type: "INTEGER"},
	}
}
