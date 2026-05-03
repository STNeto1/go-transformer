package workflow

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"

	"go-transformer/internal/pipeline"
)

func TestRun_DataSourceFilter_AllAndAny(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n2,Bob,45,US\n3,Carla,55,BR\n4,Dan,38,US\n")

	payloadAll := []byte(`{
		"pipeline_id": "wf_all",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "` + csvPath + `", "mode": "infer"}},
			{"id": "f", "type": "Filter", "inputs": ["src"], "config": {"mode": "all", "rules": [
				{"column": "country", "operation": "eq", "value": "BR"},
				{"column": "age", "operation": "gt", "value": 30}
			]}}
		],
		"sinks": [{"node_id": "f", "target_table": "out_all"}]
	}`)

	specAll, err := pipeline.ParseAndValidateJSON(payloadAll)
	require.NoError(t, err)

	resultsAll, err := Run(db, specAll)
	require.NoError(t, err)
	require.Len(t, resultsAll, 1)
	require.Equal(t, 1, resultsAll[0].RowCount)

	payloadAny := []byte(`{
		"pipeline_id": "wf_any",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "` + csvPath + `", "mode": "infer"}},
			{"id": "f", "type": "Filter", "inputs": ["src"], "config": {"mode": "any", "rules": [
				{"column": "country", "operation": "eq", "value": "BR"},
				{"column": "age", "operation": "gt", "value": 50}
			]}}
		],
		"sinks": [{"node_id": "f", "target_table": "out_any"}]
	}`)

	specAny, err := pipeline.ParseAndValidateJSON(payloadAny)
	require.NoError(t, err)

	resultsAny, err := Run(db, specAny)
	require.NoError(t, err)
	require.Len(t, resultsAny, 1)
	require.Equal(t, 2, resultsAny[0].RowCount)
}

func TestRun_UnsupportedNodeReturnsStructuredError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	payload := []byte(`{
		"pipeline_id": "wf_panic",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "` + csvPath + `", "mode": "infer"}},
			{"id": "s", "type": "Sort", "inputs": ["src"], "config": {"keys": [{"column": "age", "direction": "desc"}]}}
		],
		"sinks": [{"node_id": "s", "target_table": "out"}]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)

	// force a structurally valid spec that contains unsupported node type
	spec = &pipeline.Spec{
		PipelineID: "wf_panic",
		Version:    1,
		Nodes: []pipeline.Node{
			{ID: "src", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
			{ID: "x", Type: "NotSupportedYet", Inputs: []byte(`["src"]`), Config: []byte(`{"ok":true}`)},
		},
		Sinks: []pipeline.Sink{{NodeID: "x", TargetTable: "out"}},
	}

	_, err = Run(db, spec)
	require.Error(t, err)

	var wfErr *WorkflowError
	require.True(t, errors.As(err, &wfErr))
	require.Equal(t, ErrorCodeUnsupportedNodeType, wfErr.Code)
	require.Equal(t, "dispatch", wfErr.Stage)
	require.Equal(t, "x", wfErr.NodeID)
	require.Equal(t, "NotSupportedYet", wfErr.NodeType)
}

func TestRun_MissingSinkOutputRefReturnsStructuredError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	spec := &pipeline.Spec{
		PipelineID: "wf_sink",
		Version:    1,
		Nodes: []pipeline.Node{
			{ID: "src", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
		},
		Sinks: []pipeline.Sink{{NodeID: "missing", TargetTable: "out"}},
	}

	_, err := Run(db, spec)
	require.Error(t, err)

	var wfErr *WorkflowError
	require.True(t, errors.As(err, &wfErr))
	require.Equal(t, ErrorCodeSinkResolution, wfErr.Code)
	require.Equal(t, "resolve_sink", wfErr.Stage)
}

func TestRun_TableNameCollisionReturnsStructuredError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	spec := &pipeline.Spec{
		PipelineID: "wf collision",
		Version:    1,
		Nodes: []pipeline.Node{
			{ID: "A B", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
			{ID: "A_B", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
		},
		Sinks: []pipeline.Sink{{NodeID: "A_B", TargetTable: "out"}},
	}

	_, err := Run(db, spec)
	require.Error(t, err)

	var wfErr *WorkflowError
	require.True(t, errors.As(err, &wfErr))
	require.Equal(t, ErrorCodeTableNameCollision, wfErr.Code)
	require.Equal(t, "table_name", wfErr.Stage)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", ":memory:")
	require.NoError(t, err)
	return db
}

func writeCSV(t *testing.T, name string, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}
