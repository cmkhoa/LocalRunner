package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
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

func TestQueueSkipsCanceledQueuedWork(t *testing.T) {
	queue := New[int](1)
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, _, err := queue.Submit(context.Background(), func(context.Context) (int, error) {
			close(entered)
			<-release
			return 1, nil
		})
		firstDone <- err
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secondDone := make(chan error, 1)
	secondRan := make(chan struct{}, 1)
	go func() {
		_, _, err := queue.Submit(ctx, func(context.Context) (int, error) {
			secondRan <- struct{}{}
			return 2, nil
		})
		secondDone <- err
	}()

	deadline := time.After(time.Second)
	for len(queue.jobs) != 1 {
		select {
		case <-deadline:
			t.Fatal("canceled request was not queued")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-secondDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}

	thirdStarted := make(chan struct{})
	if _, _, err := queue.Submit(context.Background(), func(context.Context) (int, error) {
		close(thirdStarted)
		return 3, nil
	}); err != nil {
		t.Fatal(err)
	}
	<-thirdStarted
	select {
	case <-secondRan:
		t.Fatal("canceled queued work was executed")
	default:
	}
}
