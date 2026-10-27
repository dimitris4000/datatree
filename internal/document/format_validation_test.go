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
