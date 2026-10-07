package deletion

import (
	"sync"
	"time"
)

// Run deletes targets one after another in the background. Failures do not stop it;
// Stop ends it after the current item, and the rest are reported as skipped.
type Run struct {
	lock      sync.Mutex
	targets   []Target
	outcomes  []Outcome
	permanent bool
	stopping  bool
	finished  bool
	started   time.Time
	ended     time.Time
}

// RunState is a snapshot for the UI.
type RunState struct {
	Permanent bool      `json:"permanent"`
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Stopping  bool      `json:"stopping"`
	Finished  bool      `json:"finished"`
	Outcomes  []Outcome `json:"outcomes"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitempty"`
}

// Start begins deleting in a new goroutine. onFinish runs once, after the last item.
func Start(guard *Guard, remover Remover, targets []Target, permanent bool,
	onFinish func(*Run)) *Run {
	run := &Run{targets: targets, permanent: permanent, started: time.Now()}
	go func() {
		for _, target := range targets {
			run.lock.Lock()
			stopping := run.stopping
			run.lock.Unlock()
			var outcome Outcome
			if stopping {
				outcome = Outcome{Check: Check{Status: StatusSkipped,
					Reason: "stopped before this item", Path: guard.Tree.Path(target.Node)}}
			} else {
				outcome = guard.Delete(target, remover, permanent)
			}
			run.lock.Lock()
			run.outcomes = append(run.outcomes, outcome)
			run.lock.Unlock()
		}
		run.lock.Lock()
		run.finished, run.ended = true, time.Now()
		run.lock.Unlock()
		if onFinish != nil {
			onFinish(run)
		}
	}()
	return run
}

// Stop asks the run to end after the current item.
func (run *Run) Stop() {
	run.lock.Lock()
	defer run.lock.Unlock()
	run.stopping = true
}

// Targets returns the targets in order; outcome i belongs to target i.
func (run *Run) Targets() []Target { return run.targets }

// State returns a copy of the run's progress.
func (run *Run) State() RunState {
	run.lock.Lock()
	defer run.lock.Unlock()
	return RunState{Permanent: run.permanent, Total: len(run.targets),
		Done: len(run.outcomes), Stopping: run.stopping, Finished: run.finished,
		Outcomes: append([]Outcome{}, run.outcomes...), Started: run.started,
		Ended: run.ended}
}
