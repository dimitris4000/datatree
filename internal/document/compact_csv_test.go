package document

import (
	"context"
	"reflect"
	"testing"
)

func TestStreamingCSVMatchesLegacyRecords(t *testing.T) {
	for _, source := range []string{
		"name,description\r\none,\"line1\r\nline2\"\r\n\r\n\"\",last\r\n",
		"name,other\na,\nb,\"quote\"\"😀\"\n",
		"only\n\"\"\nvalue\n",
		"name,other\n",
	} {
		legacy, err := parseLegacyWithLimitsContext(context.Background(), "csv", []byte(source), DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		want, err := legacy.compactify(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseCompactCSV(context.Background(), []byte(source), DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if got.raw != nil || got.Nodes != nil || got.NodeCount() != want.NodeCount() {
			t.Fatal("retained graph/count")
		}
		for id := range legacy.Nodes {
			a, _ := got.compact.Record(compactID(id))
			b, _ := want.compact.Record(compactID(id))
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("record %s: %+v != %+v", id, a, b)
			}
		}
		if !reflect.DeepEqual(got.headers, want.headers) {
			t.Fatal("headers reused row storage")
		}
	}
}

func TestStreamingCSVRejectsInvalidRowsAndRawNodeOverflow(t *testing.T) {
	for _, source := range []string{"", "a,a\n1,2\n", "a, \n1,2\n", "a,b\n1\n", "a\n1,2\n", "a\n\"unterminated"} {
		if _, err := ParseCompactWithLimitsContext(context.Background(), "csv", []byte(source), DefaultLimits()); err == nil {
			t.Fatalf("accepted invalid CSV %q", source)
		}
	}
	source := []byte("a,b\n1,2\n")
	limits := Limits{MaxBytes: 1024, MaxNodes: 5}
	if _, err := ParseCompactWithLimitsContext(context.Background(), "csv", source, limits); err == nil {
		t.Fatal("raw key weights ignored")
	}
	limits.MaxNodes = 6
	if _, err := ParseCompactWithLimitsContext(context.Background(), "csv", source, limits); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseCompactWithLimitsContext(ctx, "csv", source, limits); err == nil {
		t.Fatal("cancelled import accepted")
	}
}
