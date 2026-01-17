package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"forgeiq/internal/controlplane/contracts"
)

type ObservabilityAgent struct{}

func main() {
	agent := &ObservabilityAgent{}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", agent.handleDiscovery)
	mux.HandleFunc("/task", agent.handleTask)

	port := os.Getenv("OBS_AGENT_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8083"
	}
	addr := ":" + port
	log.Printf("Observability Agent listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (a *ObservabilityAgent) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	meta := map[string]any{
		"name":           "observability-agent",
		"version":        "v1",
		"kind":           "observability",
		"prometheus_url": os.Getenv("PROMETHEUS_URL"),
		"loki_url":       os.Getenv("LOKI_URL"),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (a *ObservabilityAgent) handleTask(w http.ResponseWriter, r *http.Request) {
	var req contracts.A2ATaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var in contracts.ObservabilityInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	out := a.execute(ctx, in)

	respBytes, _ := json.Marshal(out)
	resp := contracts.A2ATaskResponse{
		Status: "ok",
		Output: respBytes,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (a *ObservabilityAgent) execute(ctx context.Context, in contracts.ObservabilityInput) contracts.ObservabilityOutput {
	qtype := strings.ToLower(strings.TrimSpace(in.QueryType))
	q := strings.TrimSpace(in.Query)
	if qtype == "" {
		qtype = "promql"
	}
	if q == "" {
		return contracts.ObservabilityOutput{
			Engine:   "stub",
			Degraded: true,
			Error:    "query is empty",
			Summary:  "No query provided",
		}
	}

	switch qtype {
	case "promql", "prometheus":
		promURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PROMETHEUS_URL")), "/")
		if promURL == "" {
			return contracts.ObservabilityOutput{
				Engine:   "stub",
				Degraded: true,
				Error:    "PROMETHEUS_URL not configured",
				Summary:  "Prometheus not configured; cannot execute promql query",
				Signals:  map[string]float64{},
				Raw:      map[string]any{"query": q},
			}
		}
		return queryPrometheus(ctx, promURL, q, in.Params)

	case "logs", "loki":
		lokiURL := strings.TrimRight(strings.TrimSpace(os.Getenv("LOKI_URL")), "/")
		if lokiURL == "" {
			return contracts.ObservabilityOutput{
				Engine:   "stub",
				Degraded: true,
				Error:    "LOKI_URL not configured",
				Summary:  "Loki not configured; cannot execute log query",
				Signals:  map[string]float64{},
				Raw:      map[string]any{"query": q},
			}
		}
		return queryLoki(ctx, lokiURL, q, in.Params)

	default:
		return contracts.ObservabilityOutput{
			Engine:   "stub",
			Degraded: true,
			Error:    "unsupported query_type: " + qtype,
			Summary:  "Unsupported query type: " + qtype,
			Raw:      map[string]any{"query_type": qtype, "query": q},
		}
	}
}

func queryPrometheus(ctx context.Context, baseURL, query string, params map[string]string) contracts.ObservabilityOutput {
	if shouldRangeQuery(params) {
		return queryPrometheusRange(ctx, baseURL, query, params)
	}

	u, _ := url.Parse(baseURL + "/api/v1/query")
	qp := u.Query()
	qp.Set("query", query)
	u.RawQuery = qp.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return contracts.ObservabilityOutput{Engine: "prometheus", Degraded: true, Error: err.Error(), Summary: "Prometheus query failed"}
	}
	defer resp.Body.Close()

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return contracts.ObservabilityOutput{Engine: "prometheus", Degraded: true, Error: err.Error(), Summary: "Prometheus response decode failed"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contracts.ObservabilityOutput{Engine: "prometheus", Degraded: true, Error: fmt.Sprintf("http %d", resp.StatusCode), Summary: "Prometheus returned error", Raw: raw}
	}

	signals := parsePrometheusSignalsInstant(raw)
	summary := summarizeSignals("Prometheus", signals)
	return contracts.ObservabilityOutput{
		Engine:  "prometheus",
		Summary: summary,
		Signals: signals,
		Raw:     raw,
	}
}

func queryPrometheusRange(ctx context.Context, baseURL, query string, params map[string]string) contracts.ObservabilityOutput {
	start, end, step, err := resolveRangeParams(time.Now(), params)
	if err != nil {
		return contracts.ObservabilityOutput{Engine: "prometheus", Degraded: true, Error: err.Error(), Summary: "Prometheus range params invalid"}
	}

	u, _ := url.Parse(baseURL + "/api/v1/query_range")
	qp := u.Query()
	qp.Set("query", query)
	qp.Set("start", formatUnixSeconds(start))
	qp.Set("end", formatUnixSeconds(end))
	qp.Set("step", step)
	u.RawQuery = qp.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return contracts.ObservabilityOutput{Engine: "prometheus", Degraded: true, Error: err.Error(), Summary: "Prometheus range query failed"}
	}
	defer resp.Body.Close()

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return contracts.ObservabilityOutput{Engine: "prometheus", Degraded: true, Error: err.Error(), Summary: "Prometheus range response decode failed"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contracts.ObservabilityOutput{Engine: "prometheus", Degraded: true, Error: fmt.Sprintf("http %d", resp.StatusCode), Summary: "Prometheus range returned error", Raw: raw}
	}

	signals := parsePrometheusSignalsRange(raw, parseBool(params["trend"]))
	summary := summarizeSignals("Prometheus", signals)
	return contracts.ObservabilityOutput{
		Engine:  "prometheus",
		Summary: summary,
		Signals: signals,
		Raw:     raw,
	}
}

func queryLoki(ctx context.Context, baseURL, query string, params map[string]string) contracts.ObservabilityOutput {
	if shouldRangeQuery(params) {
		return queryLokiRange(ctx, baseURL, query, params)
	}

	u, _ := url.Parse(baseURL + "/loki/api/v1/query")
	qp := u.Query()
	qp.Set("query", query)
	u.RawQuery = qp.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return contracts.ObservabilityOutput{Engine: "loki", Degraded: true, Error: err.Error(), Summary: "Loki query failed"}
	}
	defer resp.Body.Close()

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return contracts.ObservabilityOutput{Engine: "loki", Degraded: true, Error: err.Error(), Summary: "Loki response decode failed"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contracts.ObservabilityOutput{Engine: "loki", Degraded: true, Error: fmt.Sprintf("http %d", resp.StatusCode), Summary: "Loki returned error", Raw: raw}
	}

	signals := parseLokiSignals(raw, time.Time{}, time.Time{}, parseBool(params["trend"]))
	summary := summarizeSignals("Loki", signals)
	return contracts.ObservabilityOutput{
		Engine:  "loki",
		Summary: summary,
		Signals: signals,
		Raw:     raw,
	}
}

func queryLokiRange(ctx context.Context, baseURL, query string, params map[string]string) contracts.ObservabilityOutput {
	start, end, step, err := resolveRangeParams(time.Now(), params)
	if err != nil {
		return contracts.ObservabilityOutput{Engine: "loki", Degraded: true, Error: err.Error(), Summary: "Loki range params invalid"}
	}

	u, _ := url.Parse(baseURL + "/loki/api/v1/query_range")
	qp := u.Query()
	qp.Set("query", query)
	qp.Set("start", formatUnixNano(start))
	qp.Set("end", formatUnixNano(end))
	qp.Set("step", step)
	u.RawQuery = qp.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return contracts.ObservabilityOutput{Engine: "loki", Degraded: true, Error: err.Error(), Summary: "Loki range query failed"}
	}
	defer resp.Body.Close()

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return contracts.ObservabilityOutput{Engine: "loki", Degraded: true, Error: err.Error(), Summary: "Loki range response decode failed"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contracts.ObservabilityOutput{Engine: "loki", Degraded: true, Error: fmt.Sprintf("http %d", resp.StatusCode), Summary: "Loki range returned error", Raw: raw}
	}

	signals := parseLokiSignals(raw, start, end, parseBool(params["trend"]))
	summary := summarizeSignals("Loki", signals)
	return contracts.ObservabilityOutput{
		Engine:  "loki",
		Summary: summary,
		Signals: signals,
		Raw:     raw,
	}
}

func parsePrometheusSignalsInstant(raw map[string]any) map[string]float64 {
	// Prometheus: {"status":"success","data":{"resultType":"vector","result":[{"value":[ts,"123.4"],...}]}}
	data, _ := raw["data"].(map[string]any)
	res, _ := data["result"].([]any)
	if len(res) == 0 {
		return map[string]float64{}
	}

	sum := 0.0
	series := 0.0
	for _, item := range res {
		m, _ := item.(map[string]any)
		valAny, _ := m["value"].([]any)
		if len(valAny) < 2 {
			continue
		}
		if v, ok := parseFloat(valAny[1]); ok {
			sum += v
			series++
		}
	}
	if series == 0 {
		return map[string]float64{}
	}
	return map[string]float64{
		"value":  sum, // preserve legacy gate key
		"sum":    sum,
		"series": series,
	}
}

func parsePrometheusSignalsRange(raw map[string]any, includeTrend bool) map[string]float64 {
	// Prometheus range: {"status":"success","data":{"resultType":"matrix","result":[{"values":[[ts,"v"],...]},...]}}
	ts, ys, series := extractMatrixAsSummedSeries(raw)
	if len(ts) == 0 || len(ys) == 0 {
		return map[string]float64{}
	}
	stats := seriesStats(ts, ys, includeTrend)
	stats["series"] = float64(series)
	// Back-compat for gate logic: set "value" = last.
	if last, ok := stats["last"]; ok {
		stats["value"] = last
	}
	return stats
}

func parseLokiSignals(raw map[string]any, start, end time.Time, includeTrend bool) map[string]float64 {
	data, _ := raw["data"].(map[string]any)
	if data == nil {
		return map[string]float64{}
	}
	rt, _ := data["resultType"].(string)
	res, _ := data["result"].([]any)
	if res == nil {
		return map[string]float64{}
	}

	switch rt {
	case "streams":
		// Streams: result=[{"stream":{...},"values":[[ts,line],...]}]
		streams := len(res)
		lines := 0
		for _, item := range res {
			m, _ := item.(map[string]any)
			vals, _ := m["values"].([]any)
			lines += len(vals)
		}
		sigs := map[string]float64{
			"streams": float64(streams),
			"lines":   float64(lines),
			"value":   float64(lines), // provide a numeric decision signal
		}
		// If we know the window, also report rate.
		if !start.IsZero() && !end.IsZero() && end.After(start) {
			sigs["lines_per_s"] = float64(lines) / end.Sub(start).Seconds()
		}
		return sigs

	case "vector", "matrix":
		// Loki metric queries can return numeric values like Prometheus.
		if rt == "vector" {
			// Sum vector values
			sum := 0.0
			series := 0.0
			for _, item := range res {
				m, _ := item.(map[string]any)
				valAny, _ := m["value"].([]any)
				if len(valAny) < 2 {
					continue
				}
				if v, ok := parseFloat(valAny[1]); ok {
					sum += v
					series++
				}
			}
			if series == 0 {
				return map[string]float64{}
			}
			return map[string]float64{
				"value":  sum,
				"sum":    sum,
				"series": series,
			}
		}
		ts, ys, series := extractMatrixAsSummedSeries(raw)
		if len(ts) == 0 || len(ys) == 0 {
			return map[string]float64{}
		}
		stats := seriesStats(ts, ys, includeTrend)
		stats["series"] = float64(series)
		if last, ok := stats["last"]; ok {
			stats["value"] = last
		}
		return stats
	default:
		// Unknown result type; return something minimally useful.
		return map[string]float64{}
	}
}

func extractMatrixAsSummedSeries(raw map[string]any) ([]float64, []float64, int) {
	data, _ := raw["data"].(map[string]any)
	if data == nil {
		return nil, nil, 0
	}
	res, _ := data["result"].([]any)
	if len(res) == 0 {
		return nil, nil, 0
	}

	byTS := map[float64]float64{}
	series := 0
	for _, item := range res {
		m, _ := item.(map[string]any)
		vals, _ := m["values"].([]any)
		if vals == nil {
			continue
		}
		series++
		for _, vAny := range vals {
			pair, _ := vAny.([]any)
			if len(pair) < 2 {
				continue
			}
			t, okT := parseFloat(pair[0])
			v, okV := parseFloat(pair[1])
			if !okT || !okV {
				continue
			}
			byTS[t] += v
		}
	}
	if len(byTS) == 0 {
		return nil, nil, series
	}
	keys := make([]float64, 0, len(byTS))
	for t := range byTS {
		keys = append(keys, t)
	}
	sort.Float64s(keys)

	ts := make([]float64, 0, len(keys))
	ys := make([]float64, 0, len(keys))
	for _, t := range keys {
		ts = append(ts, t)
		ys = append(ys, byTS[t])
	}
	return ts, ys, series
}

func seriesStats(ts []float64, ys []float64, includeTrend bool) map[string]float64 {
	if len(ts) == 0 || len(ys) == 0 {
		return map[string]float64{}
	}
	minV := ys[0]
	maxV := ys[0]
	sum := 0.0
	for _, v := range ys {
		sum += v
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	avg := sum / float64(len(ys))
	last := ys[len(ys)-1]
	out := map[string]float64{
		"min":     minV,
		"max":     maxV,
		"avg":     avg,
		"last":    last,
		"samples": float64(len(ys)),
	}
	if includeTrend {
		out["slope"] = linearRegressionSlope(ts, ys) // units: value per second (if ts are unix seconds)
	}
	return out
}

func linearRegressionSlope(ts []float64, ys []float64) float64 {
	if len(ts) != len(ys) || len(ts) < 2 {
		return 0
	}
	// slope = cov(t,y)/var(t)
	meanT := 0.0
	meanY := 0.0
	for i := range ts {
		meanT += ts[i]
		meanY += ys[i]
	}
	meanT /= float64(len(ts))
	meanY /= float64(len(ys))

	varNum := 0.0
	cov := 0.0
	for i := range ts {
		dt := ts[i] - meanT
		dy := ys[i] - meanY
		varNum += dt * dt
		cov += dt * dy
	}
	if varNum == 0 {
		return 0
	}
	return cov / varNum
}

func shouldRangeQuery(params map[string]string) bool {
	if params == nil {
		return false
	}
	if v := strings.TrimSpace(params["mode"]); strings.ToLower(v) == "range" {
		return true
	}
	if strings.TrimSpace(params["range"]) != "" || strings.TrimSpace(params["range_seconds"]) != "" {
		return true
	}
	// explicit start/end/step
	return strings.TrimSpace(params["start"]) != "" && strings.TrimSpace(params["end"]) != "" && strings.TrimSpace(params["step"]) != ""
}

func resolveRangeParams(now time.Time, params map[string]string) (time.Time, time.Time, string, error) {
	step := strings.TrimSpace(params["step"])
	if step == "" {
		step = "30s"
	}

	// If explicit start/end, trust them.
	if s := strings.TrimSpace(params["start"]); s != "" && strings.TrimSpace(params["end"]) != "" {
		start, err := parseTimeFlexible(s)
		if err != nil {
			return time.Time{}, time.Time{}, "", fmt.Errorf("invalid start: %w", err)
		}
		end, err := parseTimeFlexible(strings.TrimSpace(params["end"]))
		if err != nil {
			return time.Time{}, time.Time{}, "", fmt.Errorf("invalid end: %w", err)
		}
		if !end.After(start) {
			return time.Time{}, time.Time{}, "", fmt.Errorf("end must be after start")
		}
		return start, end, step, nil
	}

	// Otherwise use range / range_seconds relative to now.
	if rs := strings.TrimSpace(params["range_seconds"]); rs != "" {
		sec, err := strconv.Atoi(rs)
		if err != nil || sec <= 0 {
			return time.Time{}, time.Time{}, "", fmt.Errorf("invalid range_seconds")
		}
		end := now
		start := end.Add(-time.Duration(sec) * time.Second)
		return start, end, step, nil
	}
	if r := strings.TrimSpace(params["range"]); r != "" {
		d, err := time.ParseDuration(r)
		if err != nil || d <= 0 {
			return time.Time{}, time.Time{}, "", fmt.Errorf("invalid range")
		}
		end := now
		start := end.Add(-d)
		return start, end, step, nil
	}

	return time.Time{}, time.Time{}, "", fmt.Errorf("missing range params")
}

func summarizeSignals(prefix string, signals map[string]float64) string {
	if len(signals) == 0 {
		return prefix + " query executed (no samples)"
	}
	// Prefer range stats if present.
	if _, ok := signals["last"]; ok {
		s := fmt.Sprintf("%s last=%.4f avg=%.4f min=%.4f max=%.4f", prefix, signals["last"], signals["avg"], signals["min"], signals["max"])
		if slope, ok := signals["slope"]; ok && !math.IsNaN(slope) && !math.IsInf(slope, 0) {
			s += fmt.Sprintf(" slope=%.6f", slope)
		}
		return s
	}
	if v, ok := signals["value"]; ok {
		return fmt.Sprintf("%s value=%.4f", prefix, v)
	}
	return prefix + " query executed"
}

func parseFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	default:
		f, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
		return f, err == nil
	}
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseTimeFlexible(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	// RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// unix seconds (int/float)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		sec := int64(f)
		nsec := int64((f - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time format")
}

func formatUnixSeconds(t time.Time) string {
	// Prometheus accepts unix seconds with decimals.
	return strconv.FormatFloat(float64(t.UnixNano())/1e9, 'f', -1, 64)
}

func formatUnixNano(t time.Time) string {
	// Loki expects unix nanoseconds (string).
	return strconv.FormatInt(t.UnixNano(), 10)
}
