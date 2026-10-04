// Package queue provides a bounded, single-worker FIFO queue.
package queue

import (
	"context"
	"errors"
	"time"
)

// ErrFull is returned immediately when a queue has reached its configured limit.
var ErrFull = errors.New("request queue is full")

// Queue serializes work to protect a single local inference runner.
type Queue[T any] struct {
	jobs chan job[T]
}

type job[T any] struct {
	ctx      context.Context
	work     func(context.Context) (T, error)
	result   chan result[T]
	enqueued time.Time
}

type result[T any] struct {
	value T
	err   error
	stats Stats
}

// Stats describes a job's wait and service intervals.
type Stats struct {
	Wait    time.Duration
	Service time.Duration
}

// New starts one worker and buffers at most capacity waiting jobs.
func New[T any](capacity int) *Queue[T] {
	q := &Queue[T]{jobs: make(chan job[T], capacity)}
	go q.run()
	return q
}

// Submit enqueues work in FIFO order and waits for its result. A canceled job is
// skipped by the worker, so callers that time out while waiting do not cause an
// unnecessary upstream request.
func (q *Queue[T]) Submit(ctx context.Context, work func(context.Context) (T, error)) (T, Stats, error) {
	var zero T
	job := job[T]{ctx: ctx, work: work, result: make(chan result[T], 1), enqueued: time.Now()}
	select {
	case q.jobs <- job:
	case <-ctx.Done():
		return zero, Stats{}, ctx.Err()
	default:
		return zero, Stats{}, ErrFull
	}
	select {
	case result := <-job.result:
		return result.value, result.stats, result.err
	case <-ctx.Done():
		return zero, Stats{}, ctx.Err()
	}
}

func (q *Queue[T]) run() {
	for job := range q.jobs {
		stats := Stats{Wait: time.Since(job.enqueued)}
		if err := job.ctx.Err(); err != nil {
			job.result <- result[T]{err: err, stats: stats}
			continue
		}
		started := time.Now()
		value, err := job.work(job.ctx)
		stats.Service = time.Since(started)
		job.result <- result[T]{value: value, err: err, stats: stats}
	}
}
