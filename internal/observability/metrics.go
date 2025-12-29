package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestSize     *prometheus.HistogramVec
	HTTPResponseSize    *prometheus.HistogramVec

	// Agent metrics
	AgentCallsTotal   *prometheus.CounterVec
	AgentCallDuration *prometheus.HistogramVec
	AgentCallErrors   *prometheus.CounterVec

	// Tool metrics
	ToolCallsTotal   *prometheus.CounterVec
	ToolCallDuration *prometheus.HistogramVec
	ToolCallErrors   *prometheus.CounterVec

	// Workflow metrics
	WorkflowStarted   *prometheus.CounterVec
	WorkflowCompleted *prometheus.CounterVec
	WorkflowFailed    *prometheus.CounterVec
	WorkflowDuration  *prometheus.HistogramVec

	// Policy metrics
	PolicyEvaluations *prometheus.CounterVec
	PolicyDenials     *prometheus.CounterVec
}

// NewMetrics creates and registers all Prometheus metrics
func NewMetrics() *Metrics {
	return &Metrics{
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "endpoint"},
		),
		HTTPRequestSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_size_bytes",
				Help:    "HTTP request size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 7),
			},
			[]string{"method", "endpoint"},
		),
		HTTPResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 7),
			},
			[]string{"method", "endpoint"},
		),
		AgentCallsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "agent_calls_total",
				Help: "Total number of agent calls",
			},
			[]string{"agent_type", "status"},
		),
		AgentCallDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "agent_call_duration_seconds",
				Help:    "Agent call duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"agent_type"},
		),
		AgentCallErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "agent_call_errors_total",
				Help: "Total number of agent call errors",
			},
			[]string{"agent_type", "error_type"},
		),
		ToolCallsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tool_calls_total",
				Help: "Total number of tool calls",
			},
			[]string{"tool_name", "tool_version", "status"},
		),
		ToolCallDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tool_call_duration_seconds",
				Help:    "Tool call duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"tool_name", "tool_version"},
		),
		ToolCallErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tool_call_errors_total",
				Help: "Total number of tool call errors",
			},
			[]string{"tool_name", "tool_version", "error_type"},
		),
		WorkflowStarted: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_started_total",
				Help: "Total number of workflows started",
			},
			[]string{"workflow_type"},
		),
		WorkflowCompleted: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_completed_total",
				Help: "Total number of workflows completed",
			},
			[]string{"workflow_type", "status"},
		),
		WorkflowFailed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "workflow_failed_total",
				Help: "Total number of failed workflows",
			},
			[]string{"workflow_type", "error_type"},
		),
		WorkflowDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "workflow_duration_seconds",
				Help:    "Workflow duration in seconds",
				Buckets: prometheus.ExponentialBuckets(1, 2, 10),
			},
			[]string{"workflow_type"},
		),
		PolicyEvaluations: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "policy_evaluations_total",
				Help: "Total number of policy evaluations",
			},
			[]string{"task_type", "result"},
		),
		PolicyDenials: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "policy_denials_total",
				Help: "Total number of policy denials",
			},
			[]string{"task_type", "reason"},
		),
	}
}

