// Package perftest supplies deterministic, non-private performance fixtures.
package perftest

import (
	"errors"
	"io"
	"strings"
)

// WriteFixture writes complete records up to the requested byte budget. Data
// includes nested collections and escaped Unicode; CSV has quoted newlines.
// The final size is within one record of size. It does not allocate size bytes.
func WriteFixture(w io.Writer, format string, size int) error {
	value := strings.Repeat("representative value ", 22)
	var prefix, record, separator, suffix string
	switch format {
	case "json":
		prefix, separator, suffix = "{\"rows\":[\n", ",\n", "\n]}\n"
		record = `{"name":"service-α","enabled":true,"count":9007199254740993,"settings":{"replicas":3,"tags":["api","prod"]},"description":"` + value + `"}`
	case "jsonl":
		separator, suffix = "\n", "\n"
		record = `{"name":"service-α","enabled":true,"count":9007199254740993,"settings":{"replicas":3,"tags":["api","prod"]},"description":"` + value + `"}`
	case "yaml":
		prefix = "rows:\n"
		record = "  - name: service-α\n    enabled: true\n    count: 9007199254740993\n    settings:\n      replicas: 3\n      tags: [api, prod]\n    description: " + value + "\n"
	case "csv":
		prefix = "name,enabled,count,description\r\n"
		record = "service-α,true,9007199254740993,\"" + value + "\ncontinued\"\r\n"
	default:
		return errors.New("fixture format must be json, jsonl, yaml or csv")
	}
	if size < len(prefix)+len(record)+len(suffix) {
		return errors.New("fixture byte budget must fit one record")
	}
	if _, err := io.WriteString(w, prefix); err != nil {
		return err
	}
	used, first := len(prefix), true
	for used+len(record)+len(suffix) <= size {
		if !first {
			if used+len(separator)+len(record)+len(suffix) > size {
				break
			}
			if _, err := io.WriteString(w, separator); err != nil {
				return err
			}
			used += len(separator)
		}
		if _, err := io.WriteString(w, record); err != nil {
			return err
		}
		used += len(record)
		first = false
	}
	_, err := io.WriteString(w, suffix)
	return err
}
