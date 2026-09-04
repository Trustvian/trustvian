// Command external-destination ports docs/use-cases.md's
// "Service-to-service security" scenario: an order service normally
// calls a known internal RPC dependency. The baseline is matured with 25
// Analyze+Observe calls against that normal event (mirroring
// docs/sdk-guide.md's "watching trust mature" pattern), then a sudden
// external call to a secrets manager, at lower identity confidence, is
// analyzed. See README.md for real captured output.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
)

func reserveEvent(id string) event.Event {
	return event.Event{
		ID:        id,
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "order-service",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.97,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryRPC,
			Name:     "InventoryService.Reserve",
		},
		Target:     event.Target{Name: "inventory-service"},
		Context:    event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 15},
	}
}

func main() {
	engine := trustvian.NewEngine()
	ctx := context.Background()

	for i := 1; i <= 25; i++ {
		result, err := engine.Analyze(ctx, reserveEvent(fmt.Sprintf("warm-up-%d", i)))
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

	secretsAccess := event.Event{
		ID:        "evt-4",
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "order-service",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.4,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryExternal,
			Name:     "GET /secret",
		},
		Target:     event.Target{Name: "secrets-manager"},
		Context:    event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 20},
	}

	result, err := engine.Analyze(ctx, secretsAccess)
	if err != nil {
		log.Fatalf("analyze: %v", err)
	}

	fmt.Println()
	fmt.Println(result.Explain())
}
