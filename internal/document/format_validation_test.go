package document

import "testing"

func TestCSVSchemaValidationUsesRowsAndStringCells(t *testing.T) {
	d, result := schemaCheck(t, "csv", "name,count\nAlice,007\nBob,2\n", `{"type":"array","items":{"type":"object","properties":{"name":{"enum":["Alice"]},"count":{"type":"string"}}}}`)
	if !result.Complete || result.Valid || len(result.Diagnostics) != 1 {
		t.Fatalf("%+v", result)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.InstancePath != "/1/name" || diagnostic.Target != d.Root.Children[1].Children[0].ID || !result.Marks[d.Root.ID].Contains {
		t.Fatalf("wrong CSV validation target: %+v", diagnostic)
	}
	_, valid := schemaCheck(t, "csv", "name,count\nAlice,007\n", `{"items":{"properties":{"count":{"const":"007"}}}}`)
	if !valid.Complete || !valid.Valid {
		t.Fatalf("CSV strings were coerced: %+v", valid)
	}
}

func TestJSONLSchemaValidatesEachRecordAndTargetsItsLine(t *testing.T) {
	d, result := schemaCheck(t, "jsonl", "{\"name\":\"Alice\"}\n\n{\"name\":\"Bob\"}\n", `{"type":"object","properties":{"name":{"const":"Alice"}}}`)
	if result.Valid || !result.Complete || len(result.Diagnostics) != 1 {
		t.Fatalf("%+v", result)
	}
	e := result.Diagnostics[0]
	if e.DocumentIndex != 1 || e.InstancePath != "/name" || e.Target != d.Root.Children[1].Children[0].ID || e.Location != "line 3, column 9" {
		t.Fatalf("incorrect JSONL target: %+v", e)
	}
}
