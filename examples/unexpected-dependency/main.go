// Command unexpected-dependency ports docs/use-cases.md's "API behavioral
// anomaly" scenario: a payment gateway normally calls payment-service.
// Here the baseline is matured with 25 Analyze+Observe calls against that
// normal event (mirroring docs/sdk-guide.md's "watching trust mature"
// pattern), then one request instead reaches admin-service — the same
// route, an unexpected dependency — from a connection with much lower
// identity confidence. See README.md for real captured output.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
)

func paymentEvent(id string) event.Event {
	return event.Event{
		ID:        id,
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "payment-gateway",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.96,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryHTTP,
			Name:     "POST /payment",
		},
		Target:     event.Target{Name: "payment-service"},
		Context:    event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 110},
	}
}

func main() {
	engine := trustvian.NewEngine()
	ctx := context.Background()

	for i := 1; i <= 25; i++ {
		result, err := engine.Analyze(ctx, paymentEvent(fmt.Sprintf("warm-up-%d", i)))
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

	unexpected := paymentEvent("evt-7")
	unexpected.Actor.IdentityConfidence = 0.35
	unexpected.Target = event.Target{Name: "admin-service"}
	unexpected.Attributes = map[string]any{"duration_ms": 640}

	result, err := engine.Analyze(ctx, unexpected)
	if err != nil {
		log.Fatalf("analyze: %v", err)
	}

	fmt.Println()
	fmt.Println(result.Explain())
}
