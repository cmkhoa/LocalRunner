package queue

import (
	"context"
	"sync"
	"testing"
)

func TestQueueRunsJobsInSubmissionOrder(t *testing.T) {
	queue := New[int](2)
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var order []int

	firstDone := make(chan error, 1)
	go func() {
		_, _, err := queue.Submit(context.Background(), func(context.Context) (int, error) {
			close(entered)
			<-release
			mu.Lock()
			order = append(order, 1)
			mu.Unlock()
			return 1, nil
		})
		firstDone <- err
	}()
	<-entered

	secondDone := make(chan error, 1)
	go func() {
		_, _, err := queue.Submit(context.Background(), func(context.Context) (int, error) {
			mu.Lock()
			order = append(order, 2)
			mu.Unlock()
			return 2, nil
		})
		secondDone <- err
	}()
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("order = %v, want [1 2]", order)
	}
}
