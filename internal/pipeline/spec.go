package pipeline

import "encoding/json"

type Spec struct {
	PipelineID string   `json:"pipeline_id"`
	Version    int      `json:"version"`
	Defaults   Defaults `json:"defaults,omitempty"`
	Nodes      []Node   `json:"nodes"`
	Sinks      []Sink   `json:"sinks"`
}

type Defaults struct {
	TelemetryFormat string `json:"telemetry_format,omitempty"`
	TelemetryLevel  string `json:"telemetry_level,omitempty"`
}

type Sink struct {
	NodeID      string `json:"node_id"`
	TargetTable string `json:"target_table"`
}

type Node struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Inputs json.RawMessage `json:"inputs,omitempty"`
	Config json.RawMessage `json:"config"`
}

type DataSourceConfig struct {
	Format  string       `json:"format"`
	Path    string       `json:"path"`
	Mode    string       `json:"mode"`
	Columns []ColumnSpec `json:"columns,omitempty"`
}

type ColumnSpec struct {
	Name    string `json:"name"`
	SQLType string `json:"sql_type"`
}

type FilterConfig struct {
	Mode  string       `json:"mode,omitempty"`
	Rules []FilterRule `json:"rules"`
}

type FilterRule struct {
	Column    string `json:"column"`
	Operation string `json:"operation"`
	Value     any    `json:"value"`
}

type AggregateConfig struct {
	GroupBy []string          `json:"group_by"`
	Metrics []AggregateMetric `json:"metrics"`
}

type AggregateMetric struct {
	Function string `json:"function"`
	Column   string `json:"column"`
	As       string `json:"as"`
}

type MergeUnionConfig struct {
	Mode string `json:"mode"`
}

type JoinConfig struct {
	Mode string    `json:"mode"`
	Keys []JoinKey `json:"keys"`
}

type JoinKey struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type JoinInputs struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}
