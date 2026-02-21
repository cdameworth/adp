package context

// LayerType represents the type of context layer
type LayerType string

const (
	LayerEssential     LayerType = "essential"
	LayerTaskRelevant  LayerType = "task_relevant"
	LayerSupporting    LayerType = "supporting"
	LayerDeepReference LayerType = "deep_reference"
)

// ContextLayer represents a single layer of context
type ContextLayer struct {
	Type    LayerType
	Content string
	Tokens  int
}

// Context represents the assembled context for an agent
type Context struct {
	Layers      []ContextLayer
	TotalTokens int
}

// BudgetConfig defines the token budget for each layer
type BudgetConfig struct {
	Essential    int
	TaskRelevant int
	Supporting   int
	Reserved     int
}

// DefaultBudget returns the default token budget configuration
func DefaultBudget() BudgetConfig {
	return BudgetConfig{
		Essential:    4000,
		TaskRelevant: 12000,
		Supporting:   8000,
		Reserved:     8000,
	}
}

// Assembler handles the assembly of context layers based on budget
type Assembler struct {
	config BudgetConfig
}

// NewAssembler creates a new Context Assembler
func NewAssembler(config BudgetConfig) *Assembler {
	return &Assembler{config: config}
}

// Assemble constructs the context from available layers, respecting the budget
func (a *Assembler) Assemble(layers []ContextLayer) (*Context, error) {
	ctx := &Context{
		Layers: make([]ContextLayer, 0),
	}

	// Group layers by type
	byType := make(map[LayerType][]ContextLayer)
	for _, l := range layers {
		byType[l.Type] = append(byType[l.Type], l)
	}

	// Process Essential layers (Must include if possible)
	for _, l := range byType[LayerEssential] {
		if ctx.TotalTokens+l.Tokens > a.config.Essential {
			// In a real implementation, we might truncate or error.
			// For now, we'll log a warning or just add it if it fits the *total* budget,
			// but strictly speaking, we should respect the layer budget.
			// Let's assume strict layer limits for this implementation.
			continue
		}
		ctx.Layers = append(ctx.Layers, l)
		ctx.TotalTokens += l.Tokens
	}

	// Process Task-Relevant layers
	for _, l := range byType[LayerTaskRelevant] {
		if ctx.TotalTokens+l.Tokens > a.config.Essential+a.config.TaskRelevant {
			continue
		}
		ctx.Layers = append(ctx.Layers, l)
		ctx.TotalTokens += l.Tokens
	}

	// Process Supporting layers
	for _, l := range byType[LayerSupporting] {
		if ctx.TotalTokens+l.Tokens > a.config.Essential+a.config.TaskRelevant+a.config.Supporting {
			continue
		}
		ctx.Layers = append(ctx.Layers, l)
		ctx.TotalTokens += l.Tokens
	}

	return ctx, nil
}
