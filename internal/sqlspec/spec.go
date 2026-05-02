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
