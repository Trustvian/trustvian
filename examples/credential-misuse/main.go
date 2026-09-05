// Command credential-misuse ports docs/use-cases.md's "Valid identity,
// abnormal behavior" scenario: a well-established, high-confidence
// service account performs an operation it has never performed before —
// a five-second bulk export. Unlike the other ported examples, the
// use-cases.md section this reuses shows a single event, not a
// normal/anomalous pair, so there is no baseline-maturity loop here: this
// is deliberately a cold-start call, faithfully matching the source
// scenario. See README.md for why full novelty from a trusted identity
// does not default to a block.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
)

func main() {
	engine := trustvian.NewEngine()
	ctx := context.Background()

	bulkExport := event.Event{
		ID:        "evt-5",
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "order-service",
			Type:               event.ActorTypeServiceAccount,
			IdentityConfidence: 0.98,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryDB,
			Name:     "EXPORT accounts_bulk",
		},
		Target:     event.Target{Name: "payment-db"},
		Context:    event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 5000},
	}

	result, err := engine.Analyze(ctx, bulkExport)
	if err != nil {
		log.Fatalf("analyze: %v", err)
	}

	fmt.Println(result.Explain())

	if _, err := engine.Observe(ctx, result); err != nil {
		log.Fatalf("observe: %v", err)
	}
}
