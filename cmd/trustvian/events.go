package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Trustvian/trustvian/event"
)

// loadEvents reads a JSON array of events from path.
func loadEvents(path string) ([]event.Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var events []event.Event
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("parse %s: %w (expected a JSON array of events)", path, err)
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("%s contains no events", path)
	}
	return events, nil
}
