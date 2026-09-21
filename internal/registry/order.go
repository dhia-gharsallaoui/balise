package registry

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Order is the type ordering used when grouping pages for an agent.
type Order struct{ names []string }

// LoadOrder reads defaults/order.yaml.
func LoadOrder(path string) (*Order, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var raw struct {
		AgentOrder []string `yaml:"agent_order"`
	}
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(raw.AgentOrder) == 0 {
		return nil, fmt.Errorf("parse %s: agent_order is missing or empty", path)
	}
	return &Order{names: raw.AgentOrder}, nil
}

// Names returns the order as declared.
func (o *Order) Names() []string { return append([]string(nil), o.names...) }

// Rank returns a sort key; unknown types sort last.
func (o *Order) Rank(typeName string) int {
	for i, name := range o.names {
		if name == typeName {
			return i
		}
	}
	return len(o.names)
}
