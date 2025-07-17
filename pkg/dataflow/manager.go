package dataflow

import (
    "context"
    "sync"
    "github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
    "github.com/yairfalse/tapio/pkg/domain"
)

// Manager handles multiple collectors
type Manager struct {
    dataflows []*DataFlow
    findings  chan *domain.Finding
    mu        sync.RWMutex
}

// NewManager creates a dataflow manager
func NewManager() *Manager {
    return &Manager{
        dataflows: make([]*DataFlow, 0),
        findings:  make(chan *domain.Finding, 1000),
    }
}

// AddCollector adds a collector to the dataflow
func (m *Manager) AddCollector(collector core.Collector) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    df := NewDataFlow(collector)
    m.dataflows = append(m.dataflows, df)
}

// Start begins all data flows
func (m *Manager) Start(ctx context.Context) error {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    for _, df := range m.dataflows {
        if err := df.Start(ctx); err != nil {
            return err
        }
        
        // Forward findings from each dataflow to manager
        go m.forwardFindings(ctx, df)
    }
    return nil
}

// forwardFindings forwards findings from individual dataflows
func (m *Manager) forwardFindings(ctx context.Context, df *DataFlow) {
    for {
        select {
        case finding := <-df.Findings():
            select {
            case m.findings <- finding:
            default:
                // Channel full, drop finding
            }
        case <-ctx.Done():
            return
        }
    }
}

// Findings returns consolidated findings
func (m *Manager) Findings() <-chan *domain.Finding {
    return m.findings
}

// Stop gracefully shuts down all flows
func (m *Manager) Stop() error {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    for _, df := range m.dataflows {
        df.Stop()
    }
    close(m.findings)
    return nil
}
