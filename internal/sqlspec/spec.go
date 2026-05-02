package sqlspec

type ColumnSpec struct {
	Name    string
	SQLType string
}

type TableSpec struct {
	Name    string
	Columns []ColumnSpec
}

type InsertSpec struct {
	Table   string
	Columns []string
	Rows    []map[string]any
}

type SelectSpec struct {
	Table   string
	Columns []string
}

const (
	SchemaOpAddColumn  = "add_column"
	SchemaOpDropColumn = "drop_column"
	SchemaOpRename     = "rename_column"
	SchemaOpChangeType = "change_type"
)

type SchemaOp struct {
	Type        string
	Column      ColumnSpec
	ColumnName  string
	NewName     string
	NewSQLType  string
	StaticValue any
}

type TableBranchSpec struct {
	TargetTable string
	Ops         []SchemaOp
}

type ResolvedBranch struct {
	Table        TableSpec
	StaticValues map[string]any
}
