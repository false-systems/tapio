// Mock OTEL functions until Wails regenerates bindings

export interface OTELTrace {
  traceId: string
  spanCount: number
  serviceName: string
  operationName: string
  duration: number
  startTime: string
  spans: OTELSpan[]
  tags: Record<string, any>
}

export interface OTELSpan {
  spanId: string
  traceId: string
  operationName: string
  serviceName: string
  startTime: number
  duration: number
  tags: Record<string, any>
  logs: SpanLog[]
  references?: SpanRef[]
  storyId?: string
  correlationId?: string
  severity?: string
  pattern?: string
}

export interface SpanLog {
  timestamp: number
  fields: LogField[]
}

export interface LogField {
  key: string
  value: any
}

export interface SpanRef {
  refType: string
  traceId: string
  spanId: string
}

// Mock implementation - replace with real Wails binding when available
export async function GetTraces(service: string, limit: number): Promise<OTELTrace[]> {
  // Call the real backend when available
  if ((window as any).go?.main?.App?.GetTraces) {
    return (window as any).go.main.App.GetTraces(service, limit)
  }
  
  // Return mock data for development
  return getMockTraces()
}

export async function GetTracesForStory(storyId: string): Promise<OTELTrace[]> {
  // Call the real backend when available
  if ((window as any).go?.main?.App?.GetTracesForStory) {
    return (window as any).go.main.App.GetTracesForStory(storyId)
  }
  
  // Return mock data filtered by story
  const allTraces = getMockTraces()
  return allTraces.filter(trace => 
    trace.spans.some(span => span.storyId === storyId)
  )
}

function getMockTraces(): OTELTrace[] {
  const now = new Date()
  
  return [
    {
      traceId: "1234567890abcdef",
      spanCount: 5,
      serviceName: "tapio-relay",
      operationName: "correlation.memory-pressure",
      duration: 2500000, // 2.5 seconds
      startTime: new Date(now.getTime() - 5 * 60000).toISOString(),
      tags: {
        "correlation.id": "corr-memory-pressure",
        "cluster": "production",
        "severity": "high",
      },
      spans: [
        {
          spanId: "span1",
          traceId: "1234567890abcdef",
          operationName: "issue.OOMKiller",
          serviceName: "tapio-relay",
          startTime: now.getTime() - 5 * 60000,
          duration: 2300,
          tags: {
            "k8s.namespace": "production",
            "k8s.pod": "api-service-abc123",
            "memory.limit": "256Mi",
            "memory.used": "256Mi",
            "pattern": "memory_pressure",
          },
          storyId: "story-001",
          correlationId: "corr-memory-pressure",
          severity: "critical",
          pattern: "memory_pressure",
          logs: [
            {
              timestamp: now.getTime() - 5 * 60000,
              fields: [
                { key: "event", value: "OOM Kill" },
                { key: "reason", value: "Memory limit exceeded" },
              ],
            },
          ],
        },
      ],
    },
  ]
}