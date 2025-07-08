package simple

import (
	"context"
	"fmt"
	"runtime"

	"github.com/falseyair/tapio/pkg/types"
)

// EnhancedExplainer combines Kubernetes API with enhanced insights
type EnhancedExplainer struct {
	*Explainer
	enableEBPF bool
}

// NewEnhancedExplainer creates an explainer with enhanced capabilities
func NewEnhancedExplainer() (*EnhancedExplainer, error) {
	baseExplainer, err := NewExplainer()
	if err != nil {
		return nil, err
	}

	explainer := &EnhancedExplainer{
		Explainer:  baseExplainer,
		enableEBPF: runtime.GOOS == "linux",
	}

	// For now, eBPF is not available in this branch
	if explainer.enableEBPF {
		fmt.Println("eBPF monitoring not available in this build")
		explainer.enableEBPF = false
	}

	return explainer, nil
}

// Explain generates a detailed explanation with enhanced insights
func (e *EnhancedExplainer) Explain(ctx context.Context, req *types.ExplainRequest) (*types.Explanation, error) {
	// Get basic explanation from base explainer
	explanation, err := e.Explainer.Explain(ctx, req)
	if err != nil {
		return nil, err
	}

	// For now, just add a note that enhanced features are available
	if e.enableEBPF {
		if explanation.Summary == "" {
			explanation.Summary = "Enhanced analysis completed (eBPF not available in this build)"
		}
	}

	return explanation, nil
}

// Close cleans up resources
func (e *EnhancedExplainer) Close() error {
	// No cleanup needed for basic version
	return nil
}