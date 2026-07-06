package scanner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/chawdamrunal/assay/internal/claude"
	"github.com/chawdamrunal/assay/internal/prompts"
	"github.com/chawdamrunal/assay/internal/tools"
)

// InvestigationInput holds dependencies for Stage 3.
type InvestigationInput struct {
	Client              claude.Client
	Model               string
	ThreatModel         ThreatModel
	MaxConcurrency      int
	MaxTurnsPerSubagent int

	// SubagentDefs and SubagentHandlers are the tool layer each investigator gets.
	// (read_file, list_dir, grep, parse_manifest, secret_scan, osv_lookup — but NOT
	// dispatch_subagent or record_finding; record_finding is injected by the dispatcher.)
	SubagentDefs     []claude.ToolDef
	SubagentHandlers map[string]claude.ToolHandler
}

// RunInvestigation executes Stage 3 — for each threat, dispatch an investigator
// sub-agent, collect findings (attributed to their threat_id), and return them
// along with any open questions (e.g., budget exceeded mid-investigation).
func RunInvestigation(ctx context.Context, in InvestigationInput) ([]tools.Finding, []string, error) {
	if len(in.ThreatModel.Threats) == 0 {
		return nil, nil, nil
	}

	system, err := prompts.Load(prompts.Version, "investigator")
	if err != nil {
		return nil, nil, fmt.Errorf("investigator: load prompt: %w", err)
	}

	parent := tools.NewFindings()
	dispatcher := tools.NewDispatcher(tools.DispatcherConfig{
		Client:           in.Client,
		Model:            in.Model,
		System:           system,
		MaxConcurrency:   in.MaxConcurrency,
		MaxTurns:         in.MaxTurnsPerSubagent,
		ParentFindings:   parent,
		SubagentDefs:     in.SubagentDefs,
		SubagentHandlers: in.SubagentHandlers,
	})

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		openQs []string
	)
	for _, t := range in.ThreatModel.Threats {
		threat := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			questions := make([]any, len(threat.ReviewerQuestions))
			for i, q := range threat.ReviewerQuestions {
				questions[i] = q
			}
			_, err := dispatcher.Dispatch(ctx, tools.Invocation{
				Input: map[string]any{
					"threat_id":          threat.ID,
					"threat_title":       threat.Title,
					"threat_description": threat.Description,
					"reviewer_questions": questions,
				},
			})
			if err != nil {
				mu.Lock()
				defer mu.Unlock()
				// Budget exceeded is the most common expected error; surface as open question.
				if errors.Is(err, claude.ErrBudgetExceeded) {
					openQs = append(openQs, fmt.Sprintf("Threat %s (%s): budget exceeded mid-investigation; partial findings only.", threat.ID, threat.Title))
				} else if strings.Contains(err.Error(), "budget exceeded") {
					openQs = append(openQs, fmt.Sprintf("Threat %s (%s): budget exceeded mid-investigation; partial findings only.", threat.ID, threat.Title))
				} else {
					openQs = append(openQs, fmt.Sprintf("Threat %s (%s): investigator error: %v", threat.ID, threat.Title, err))
				}
			}
		}()
	}
	wg.Wait()

	return parent.All(), openQs, nil
}
