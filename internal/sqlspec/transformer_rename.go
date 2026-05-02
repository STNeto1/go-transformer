package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type RenameColumn struct {
	From string
	To   string
}

func DeriveRenameColumns(base TableSpec, derivedTableName string, renames []RenameColumn) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(renames) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one rename is required")
	}

	fromSeen := map[string]struct{}{}
	toSeen := map[string]struct{}{}
	renameMap := map[string]string{}
	baseColSet := map[string]string{}

	for _, c := range base.Columns {
		baseColSet[strings.ToLower(c.Name)] = c.Name
	}

	for _, rename := range renames {
		if rename.From == "" || rename.To == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("rename requires from and to")
		}
		if strings.EqualFold(rename.From, rename.To) {
			return ResolvedBranch{}, nil, fmt.Errorf("rename from %q to %q is a no-op", rename.From, rename.To)
		}

		idx := slices.IndexFunc(base.Columns, func(c ColumnSpec) bool {
			return strings.EqualFold(c.Name, rename.From)
		})
		if idx == -1 {
			return ResolvedBranch{}, nil, fmt.Errorf("column %q does not exist", rename.From)
		}

		canonicalFrom := base.Columns[idx].Name
		fromKey := strings.ToLower(canonicalFrom)
		toKey := strings.ToLower(rename.To)

		if _, ok := fromSeen[fromKey]; ok {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate rename source %q", rename.From)
		}
		if _, ok := toSeen[toKey]; ok {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate rename target %q", rename.To)
		}

		fromSeen[fromKey] = struct{}{}
		toSeen[toKey] = struct{}{}
		renameMap[canonicalFrom] = rename.To
	}

	for _, c := range base.Columns {
		fromKey := strings.ToLower(c.Name)
		if _, renamed := fromSeen[fromKey]; renamed {
			continue
		}
		if _, collides := toSeen[fromKey]; collides {
			return ResolvedBranch{}, nil, fmt.Errorf("rename target collides with existing column %q", c.Name)
		}
	}

	cols := make([]ColumnSpec, len(base.Columns))
	copy(cols, base.Columns)

	sourceForTarget := map[string]string{}
	for i := range cols {
		sourceName := cols[i].Name
		if newName, ok := renameMap[sourceName]; ok {
			cols[i].Name = newName
		}
		sourceForTarget[cols[i].Name] = sourceName
	}

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: cols,
		},
	}

	selectFn := func(selectAst *ast.SelectStatement) {
		sourceCols := make([]ast.Expression, 0, len(resolved.Table.Columns))
		for _, col := range resolved.Table.Columns {
			sourceCols = append(sourceCols, &ast.Identifier{Name: sourceForTarget[col.Name]})
		}
		selectAst.Columns = sourceCols
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
