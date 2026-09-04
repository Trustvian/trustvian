// Command ai-agent ports docs/use-cases.md's "AI-agent security" scenario:
// a support agent's normal tool is a CRM lookup. The baseline is matured
// with 25 Analyze+Observe calls against that benign event (mirroring
// docs/sdk-guide.md's "watching trust mature" pattern), then a
// credentials-store access from the same agent, at lower identity
// confidence, is analyzed — a structurally identical event shape
// (Operation.Category = "tool") but a very different kind of action.
// See README.md for real captured output.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
)

func searchCustomerEvent(id string) event.Event {
	return event.Event{
		ID:        id,
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "support-agent",
			Type:               event.ActorTypeAIAgent,
			IdentityConfidence: 0.9,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryTool,
			Name:     "search_customer",
		},
		Target:     event.Target{Name: "crm-api"},
		Context:    event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 80},
	}
}

func main() {
	engine := trustvian.NewEngine()
	ctx := context.Background()

	for i := 1; i <= 25; i++ {
		result, err := engine.Analyze(ctx, searchCustomerEvent(fmt.Sprintf("warm-up-%d", i)))
		if err != nil {
			log.Fatalf("analyze warm-up %d: %v", i, err)
		}
		learned, err := engine.Observe(ctx, result)
		if err != nil {
			log.Fatalf("observe warm-up %d: %v", i, err)
		}
		if i == 1 || i == 10 || i == 20 || i == 25 {
			fmt.Printf("warm-up %2d: confidence=%.2f trust=%.2f decision=%s learned=%v\n",
				i, result.Anomaly.Confidence, result.Trust.Score, result.Decision, learned)
		}
	}

	suspicious := event.Event{
		ID:        "evt-2",
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "support-agent",
			Type:               event.ActorTypeAIAgent,
			IdentityConfidence: 0.3,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryTool,
			Name:     "get_credentials",
		},
		Target:     event.Target{Name: "credentials-store"},
		Context:    event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 45},
	}

	result, err := engine.Analyze(ctx, suspicious)
	if err != nil {
		log.Fatalf("analyze: %v", err)
	}

	fmt.Println()
	fmt.Println(result.Explain())
}
