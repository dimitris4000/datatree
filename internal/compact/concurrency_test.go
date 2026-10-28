package compact

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentVersionsShareOnlyImmutableChunks(t *testing.T) {
	tree := newSample(t)
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			version := tree
			for i := 0; i < 30; i++ {
				text := fmt.Sprint(worker*100 + i)
				fragment := []Input{{ID: 1, Kind: "number", Value: text, Start: 0, KeyStart: -1, End: len(text), Pathable: true}}
				next, _, err := version.Replace(context.Background(), 3, []byte(text), fragment, 1)
				if err != nil {
					failures <- err
					return
				}
				if r, ok := tree.Record(3); !ok || r.Value != "1" {
					failures <- fmt.Errorf("shared baseline changed: %+v", r)
					return
				}
				if r, ok := next.Record(3); !ok || r.Value != text {
					failures <- fmt.Errorf("private version lost value: %+v", r)
					return
				}
				next.LocateByte(0, 0)
				next.Path(3)
				next.Children(1, 0, 20)
				version = next
			}
		}(worker)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}
func TestPreorderChecksCancellationAfterVisitorStops(t *testing.T) {
	tree := newSample(t)
	ctx, cancel := context.WithCancel(context.Background())
	err := tree.Preorder(ctx, func(Record) bool { cancel(); return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled visitor: %v", err)
	}
}
