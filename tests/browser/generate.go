// This test utility reuses the deterministic Go performance fixtures.
package main

import (
	"flag"
	"log"
	"os"

	"datatree/internal/perftest"
)

func main() {
	format := flag.String("format", "json", "fixture format")
	mib := flag.Int("mib", 10, "fixture MiB (1..100)")
	path := flag.String("path", "", "output file")
	flag.Parse()
	if *mib < 1 || *mib > 100 || *path == "" {
		log.Fatal("require -mib 1..100 and -path")
	}
	file, err := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		log.Fatal(err)
	}
	if err := perftest.WriteFixture(file, *format, *mib*1024*1024); err != nil {
		file.Close()
		log.Fatal(err)
	}
	if err := file.Close(); err != nil {
		log.Fatal(err)
	}
}
