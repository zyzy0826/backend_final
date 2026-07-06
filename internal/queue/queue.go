package queue

import (
	"context"

	"regs/internal/judge"
)

// Job carries only identifiers; the judge loads the problem definition from the
// DB at run time, which keeps rejudge trivial and always up to date.
type Job struct {
	SubmissionID int
	OperatorID   string
	ProblemID    int
	ZipPath      string
}

type Queue struct {
	jobs chan Job
	jdg  *judge.Judge
}

func New(maxConcurrent int, jdg *judge.Judge) *Queue {
	q := &Queue{
		jobs: make(chan Job, 256),
		jdg:  jdg,
	}
	go q.run(maxConcurrent)
	return q
}

func (q *Queue) Push(job Job) {
	q.jobs <- job
}

// run pulls jobs off the queue in FIFO order and dispatches each to its own
// goroutine, using a buffered channel as a counting semaphore to cap the number
// of judge containers running at once.
func (q *Queue) run(maxConcurrent int) {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	sem := make(chan struct{}, maxConcurrent)

	for job := range q.jobs {
		sem <- struct{}{} // blocks once maxConcurrent jobs are in flight
		go func(job Job) {
			defer func() { <-sem }()
			q.jdg.RunJob(context.Background(), judge.JobInput{
				SubmissionID: job.SubmissionID,
				OperatorID:   job.OperatorID,
				ProblemID:    job.ProblemID,
				ZipPath:      job.ZipPath,
			})
		}(job)
	}
}
