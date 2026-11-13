package assistant

import "sync"

// runBudget accounts for work in Go, independent of moejs's task-level memory
// accounting. The provider may reset engine state per task; these counters do
// not reset until the run ends.
type runBudget struct {
	mu        sync.Mutex
	limits    Limits
	toolCalls int
	evidence  int
	turns     int
}

func newRunBudget(limits Limits) *runBudget {
	return &runBudget{limits: limits}
}

func (b *runBudget) noteToolCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.toolCalls >= b.limits.NoteToolCalls {
		return toolErr("budget_exceeded", "The note tool limit for this run has been reached.")
	}
	b.toolCalls++
	return nil
}

func (b *runBudget) addEvidence(bytes int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.evidence+bytes > b.limits.EvidenceBytes {
		return toolErr("budget_exceeded", "The note evidence limit for this run has been reached.")
	}
	b.evidence += bytes
	return nil
}

func (b *runBudget) modelTurn() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.turns >= b.limits.ModelTurns {
		return toolErr("budget_exceeded", "The model turn limit for this run has been reached.")
	}
	b.turns++
	return nil
}
