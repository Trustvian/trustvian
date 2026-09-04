// Command basic is the "hello world" of the Trustvian Go SDK: construct
// an Engine with no options, analyze one well-formed event, and print the
// result. See README.md for the scenario and real captured output.
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

	ev := event.Event{
		ID:        "evt-001",
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "svc-checkout",
			Type:               event.ActorTypeService,
			IdentityConfidence: 1.0,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryHTTP,
			Name:     "GET /api/orders",
		},
		Target: event.Target{Name: "orders-api"},
	}

	result, err := engine.Analyze(context.Background(), ev)
	if err != nil {
		log.Fatalf("analyze: %v", err)
	}

	fmt.Println(result.Explain())

	if _, err := engine.Observe(context.Background(), result); err != nil {
		log.Fatalf("observe: %v", err)
	}
}
