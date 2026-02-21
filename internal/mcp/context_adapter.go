package mcp

import (
	"context"

	ctxengine "github.com/adp/adp/internal/domain/context"
)

// ContextEngineAdapter adapts the context.Engine to the mcp.ContextEngine interface
type ContextEngineAdapter struct {
	engine *ctxengine.Engine
}

// NewContextEngineAdapter creates a new adapter wrapping the context engine
func NewContextEngineAdapter(engine *ctxengine.Engine) *ContextEngineAdapter {
	return &ContextEngineAdapter{engine: engine}
}

// AssembleContext converts the generic interface{} request to the typed context.ContextRequest,
// calls the engine, and returns the result as interface{}.
func (a *ContextEngineAdapter) AssembleContext(ctx context.Context, req interface{}) (interface{}, error) {
	contextReq, ok := req.(*ctxengine.ContextRequest)
	if !ok {
		return nil, ctxengine.ErrInvalidRequest
	}
	return a.engine.AssembleContext(ctx, *contextReq)
}
