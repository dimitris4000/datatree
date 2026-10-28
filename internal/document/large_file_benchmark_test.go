package document

import (
	"bytes"
	"os"
	"strconv"
	"testing"

	"datatree/internal/perftest"
)

// DATATREE_BENCH_FILE opts into a private local file without copying its data
// into fixtures or reports. DATATREE_BENCH_MIB selects generated fixture size.
func benchmarkInput(b *testing.B) (string, []byte) {
	b.Helper()
	if path := os.Getenv("DATATREE_BENCH_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		return "json", data
	}
	format := os.Getenv("DATATREE_BENCH_FORMAT")
	if format == "" {
		format = "json"
	}
	mib := 10
	if value := os.Getenv("DATATREE_BENCH_MIB"); value != "" {
		var err error
		mib, err = strconv.Atoi(value)
		if err != nil || mib < 1 || mib > 100 {
			b.Fatal("DATATREE_BENCH_MIB must be 1..100")
		}
	}
	var buf bytes.Buffer
	if err := perftest.WriteFixture(&buf, format, mib*1024*1024); err != nil {
		b.Fatal(err)
	}
	return format, buf.Bytes()
}

func benchmarkLimits() Limits { return Limits{MaxBytes: 100 * 1024 * 1024, MaxNodes: 4000000} }

func BenchmarkLargeParse(b *testing.B) {
	format, source := benchmarkInput(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := ParseWithLimits(format, source, benchmarkLimits())
		if err != nil {
			b.Fatal(err)
		}
		if d.Root == nil {
			b.Fatal("missing document")
		}
	}
}

func BenchmarkLargeSourceMap(b *testing.B) {
	format, source := benchmarkInput(b)
	d, err := ParseWithLimits(format, source, benchmarkLimits())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(d.SourceRanges()) == 0 {
			b.Fatal("missing ranges")
		}
	}
}

func BenchmarkLargeSearchAbsent(b *testing.B) {
	format, source := benchmarkInput(b)
	d, err := ParseWithLimits(format, source, benchmarkLimits())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := d.Find(FindOptions{Mode: "search", Text: "no-match-328414290", Keys: true, Values: true})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeScalarEdit(b *testing.B) {
	format, source := benchmarkInput(b)
	d, err := ParseWithLimits(format, source, benchmarkLimits())
	if err != nil {
		b.Fatal(err)
	}
	var target string
	var visit func(*Node)
	visit = func(n *Node) {
		if target != "" {
			return
		}
		if n.Kind == "string" {
			target = n.ID
			return
		}
		children, _ := d.ChildPage(n.ID, 0, 50)
		for _, c := range children {
			visit(c)
		}
	}
	visit(d.Root)
	if target == "" {
		b.Fatal("fixture has no string")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := d.Edit(Action{Op: "replace", Node: target, Text: `"updated"`, Format: "json"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeSameSpanEdit(b *testing.B) {
	format, source := benchmarkInput(b)
	d, err := ParseWithLimits(format, source, benchmarkLimits())
	if err != nil {
		b.Fatal(err)
	}
	var target *Node
	var visit func(*Node)
	visit = func(n *Node) {
		if target != nil {
			return
		}
		if n.Kind == "number" {
			target = n
			return
		}
		children, _ := d.ChildPage(n.ID, 0, 50)
		for _, c := range children {
			visit(c)
		}
	}
	visit(d.Root)
	if target == nil {
		b.Skip("fixture has no numeric scalar")
	}
	lexeme := append([]byte(nil), source[target.Start:target.End]...)
	if lexeme[len(lexeme)-1] == '0' {
		lexeme[len(lexeme)-1] = '1'
	} else {
		lexeme[len(lexeme)-1] = '0'
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Edit(Action{Op: "replace", Node: target.ID, Text: string(lexeme), Format: "json"}); err != nil {
			b.Fatal(err)
		}
	}
}
