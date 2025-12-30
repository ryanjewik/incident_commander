package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	urlpkg "net/url"
	"sync"
	"time"

	"google.golang.org/api/iterator"

	"github.com/ryanjewik/incident_commander/backend/config"
)

// CachedDatadog holds cached dashboard data per organization
type CachedDatadog struct {
	LastUpdated        time.Time                `json:"last_updated"`
	Metrics            map[string]interface{}   `json:"metrics"`
	Timeline           []map[string]interface{} `json:"timeline"`
	RecentLogs         []map[string]interface{} `json:"recent_logs"`
	StatusDistribution []map[string]interface{} `json:"status_distribution"`
	Monitors           []map[string]interface{} `json:"monitors"`
}

type DatadogService struct {
	cfg    config.Config
	client *http.Client

	fs *FirebaseService

	cacheMu sync.RWMutex
	cache   map[string]*CachedDatadog // per-org cache

	pollMu      sync.Mutex
	pollCancels map[string]chan struct{}
	// optional broadcaster called when new events/logs are available
	broadcaster func(orgID string, payload interface{})
}

func NewDatadogService(cfg config.Config, fs *FirebaseService) *DatadogService {
	return &DatadogService{
		cfg:         cfg,
		client:      &http.Client{Timeout: 10 * time.Second},
		fs:          fs,
		cache:       make(map[string]*CachedDatadog),
		pollCancels: make(map[string]chan struct{}),
	}
}

// StartPollingOrg starts a background poller for the given orgID (if not already running).
func (s *DatadogService) StartPollingOrg(orgID string, interval time.Duration) {
	if orgID == "" {
		return
	}
	s.pollMu.Lock()
	defer s.pollMu.Unlock()
	if _, ok := s.pollCancels[orgID]; ok {
		return // already polling
	}
	stop := make(chan struct{})
	s.pollCancels[orgID] = stop
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// immediate fetch
		s.fetchForOrg(context.Background(), orgID)
		for {
			select {
			case <-ticker.C:
				s.fetchForOrg(context.Background(), orgID)
			case <-stop:
				return
			}
		}
	}()
}

// StopPollingOrg stops polling for the given org
func (s *DatadogService) StopPollingOrg(orgID string) {
	s.pollMu.Lock()
	defer s.pollMu.Unlock()
	if ch, ok := s.pollCancels[orgID]; ok {
		close(ch)
		delete(s.pollCancels, orgID)
	}
}

func (s *DatadogService) fetchForOrg(ctx context.Context, orgID string) {
	// Read organization doc to obtain datadog_secrets
	docSnap, err := s.fs.Firestore.Collection("organizations").Doc(orgID).Get(ctx)
	if err != nil {
		return
	}
	data := docSnap.Data()

	// Extract encrypted fields
	var encAPI, encApp, wrappedDEK string
	if v, ok := data["datadog_secrets"].(map[string]interface{}); ok {
		if x, ok2 := v["encrypted_apiKey"].(string); ok2 {
			encAPI = x
		}
		if x, ok2 := v["encrypted_appKey"].(string); ok2 {
			encApp = x
		}
		if x, ok2 := v["encrypted_apiKeyDek"].(string); ok2 {
			wrappedDEK = x
		}
		// also accept older plain fields
		if encAPI == "" {
			if x, ok2 := v["apiKey"].(string); ok2 {
				encAPI = x
			}
		}
		if encApp == "" {
			if x, ok2 := v["appKey"].(string); ok2 {
				encApp = x
			}
		}
	}

	// If top-level fields used instead
	if encAPI == "" {
		if x, ok := data["datadog_secrets.apiKey"].(string); ok {
			encAPI = x
		}
	}
	if encApp == "" {
		if x, ok := data["datadog_secrets.appKey"].(string); ok {
			encApp = x
		}
	}

	apiKey := ""
	appKey := ""
	if wrappedDEK != "" || encAPI != "" || encApp != "" {
		// Attempt to decrypt; DecryptData returns plaintexts for provided encrypted inputs
		a, b, derr := DecryptData(encAPI, encApp, wrappedDEK)
		if derr == nil {
			apiKey = a
			appKey = b
		} else {
			// fallback: use provided strings if decryption fails
			if encAPI != "" {
				apiKey = encAPI
			}
			if encApp != "" {
				appKey = encApp
			}
		}
	}

	if apiKey == "" && appKey == "" {
		// nothing to call
		return
	}

	site := s.cfg.DataDog_SITE
	if site == "" {
		site = "api.datadoghq.com"
	}
	eventsURL := fmt.Sprintf("https://%s/api/v1/events?start=%d&end=%d", site, time.Now().Add(-1*time.Hour).Unix(), time.Now().Unix())

	req, _ := http.NewRequestWithContext(ctx, "GET", eventsURL, nil)
	if apiKey != "" {
		req.Header.Set("DD-API-KEY", apiKey)
	}
	if appKey != "" {
		req.Header.Set("DD-APPLICATION-KEY", appKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}

	events, _ := body["events"].([]interface{})
	timeline := make([]map[string]interface{}, 0)
	recentLogs := make([]map[string]interface{}, 0)

	for i, e := range events {
		if i >= 20 {
			break
		}
		if emap, ok := e.(map[string]interface{}); ok {
			title := ""
			if t, ok := emap["title"].(string); ok {
				title = t
			}
			date := time.Now().Format(time.RFC3339)
			if ts, ok := emap["date_happened"].(float64); ok {
				date = time.Unix(int64(ts), 0).Format(time.RFC3339)
			}
			recentLogs = append(recentLogs, map[string]interface{}{"time": date, "level": "INFO", "message": title, "service": "datadog_event"})
			timeline = append(timeline, map[string]interface{}{"date": date, "incidents": 1})
		}
	}

	statusDist := []map[string]interface{}{
		{"status": "Active", "count": len(events) / 4, "percentage": 40},
		{"status": "New", "count": len(events) / 6, "percentage": 20},
		{"status": "Resolved", "count": len(events) / 3, "percentage": 30},
		{"status": "Ignored", "count": len(events) / 12, "percentage": 10},
	}

	// Now try to read actual incidents from Firestore for org and build timeline/status from those.
	// This will provide accurate timeline and status distribution.
	incidentsCol := s.fs.Firestore.Collection("incidents")
	q := incidentsCol.Where("organization_id", "==", orgID)
	iter := q.Documents(ctx)
	defer iter.Stop()

	// prepare last 7 days map
	now := time.Now()
	days := make([]string, 7)
	dayCounts := make(map[string]int)
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		days[6-i] = d
		dayCounts[d] = 0
	}

	statusCounts := make(map[string]int)
	foundAny := false

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		foundAny = true
		data := doc.Data()
		// created_at may be time.Time or string
		var created time.Time
		if t, ok := data["created_at"].(time.Time); ok {
			created = t
		} else if sstr, ok := data["created_at"].(string); ok {
			if parsed, perr := time.Parse(time.RFC3339, sstr); perr == nil {
				created = parsed
			} else {
				created = time.Now()
			}
		} else {
			created = time.Now()
		}
		dayKey := created.Format("2006-01-02")
		if _, ok := dayCounts[dayKey]; ok {
			dayCounts[dayKey]++
		}

		st := "Unknown"
		if sVal, ok := data["status"].(string); ok && sVal != "" {
			st = sVal
		}
		statusCounts[st]++
	}

	if foundAny {
		// rebuild timeline from dayCounts
		timeline = make([]map[string]interface{}, 0, 7)
		total := 0
		for _, d := range days {
			c := dayCounts[d]
			timeline = append(timeline, map[string]interface{}{"date": d, "incidents": c})
			total += c
		}
		// rebuild statusDist from statusCounts
		statusDist = make([]map[string]interface{}, 0, len(statusCounts))
		for k, v := range statusCounts {
			pct := 0
			if total > 0 {
				pct = int((v * 100) / total)
			}
			statusDist = append(statusDist, map[string]interface{}{"status": k, "count": v, "percentage": pct})
		}
	}

	// Ensure each status entry has a deterministic hex color (frontend can rely on this)
	for _, sEntry := range statusDist {
		st := ""
		if sVal, ok := sEntry["status"].(string); ok {
			st = sVal
		}
		switch st {
		case "new", "New", "NEW":
			sEntry["colorHex"] = "#6366F1"
		case "active", "Active", "ACTIVE":
			sEntry["colorHex"] = "#8B5CF6"
		case "resolved", "Resolved", "RESOLVED":
			sEntry["colorHex"] = "#10B981"
		case "ignored", "Ignored", "IGNORED":
			sEntry["colorHex"] = "#9CA3AF"
		case "unknown", "Unknown", "UNKNOWN":
			sEntry["colorHex"] = "#F59E0B"
		default:
			sEntry["colorHex"] = "#9CA3AF"
		}
	}

	// Attempt to query Datadog Metrics API for more realistic metrics
	to := time.Now().Unix()
	from := time.Now().Add(-5 * time.Minute).Unix()

	// helper to query metrics
	queryMetric := func(q string) (float64, error) {
		site := s.cfg.DataDog_SITE
		if site == "" {
			site = "api.datadoghq.com"
		}
		full := fmt.Sprintf("https://%s/api/v1/query?from=%d&to=%d&query=%s", site, from, to, urlpkg.QueryEscape(q))
		req, _ := http.NewRequestWithContext(ctx, "GET", full, nil)
		if apiKey != "" {
			req.Header.Set("DD-API-KEY", apiKey)
		}
		if appKey != "" {
			req.Header.Set("DD-APPLICATION-KEY", appKey)
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return 0, fmt.Errorf("datadog query status %d", resp.StatusCode)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return 0, err
		}
		series, _ := body["series"].([]interface{})
		if len(series) == 0 {
			return 0, fmt.Errorf("no series")
		}
		first, _ := series[0].(map[string]interface{})
		// try pointlist then pointlist
		if pl, ok := first["pointlist"].([]interface{}); ok && len(pl) > 0 {
			last := pl[len(pl)-1]
			if pair, ok := last.([]interface{}); ok && len(pair) >= 2 {
				if v, ok := pair[1].(float64); ok {
					return v, nil
				}
			}
		}
		if pl2, ok := first["pointlist"].([]interface{}); ok && len(pl2) > 0 {
			last := pl2[len(pl2)-1]
			if pair, ok := last.([]interface{}); ok && len(pair) >= 2 {
				if v, ok := pair[1].(float64); ok {
					return v, nil
				}
			}
		}
		return 0, fmt.Errorf("no datapoints")
	}

	// Best-effort metric queries (may vary by host/agent configuration)
	// CPU: combine user + system to approximate percent usage
	cpuUser, _ := queryMetric("avg:system.cpu.user{*}")
	cpuSystem, _ := queryMetric("avg:system.cpu.system{*}")
	cpuVal := cpuUser + cpuSystem

	// Memory percent: used / total * 100
	memPct, _ := queryMetric("avg:system.mem.used{*} / avg:system.mem.total{*} * 100")

	// Disk IO and Network in bytes/sec (rollup over 60s)
	diskBps, _ := queryMetric("avg:rate:system.disk.read_bytes{*}.rollup(sum,60) + avg:rate:system.disk.write_bytes{*}.rollup(sum,60)")
	netBps, _ := queryMetric("avg:rate:system.net.bytes_rcvd{*}.rollup(sum,60) + avg:rate:system.net.bytes_sent{*}.rollup(sum,60)")

	// If we didn't get any series for host-level system metrics, try app-emitted
	// metrics (dummy.*) as a fallback. Some environments emit gauges for
	// bytes/sec and the Datadog `rate()` expression may not return series.
	if diskBps == 0 {
		// try app-level disk metrics (both dummy.* and plain system.* gauges)
		// many demo environments emit gauges named either `dummy.system.*` or `system.*`
		// Try various common metric names: prefer explicit rate gauges first
		// plain derived gauges emitted by the app
		db1, derr1 := queryMetric("avg:disk_bps{*}")
		dm1, derr2 := queryMetric("avg:disk_mb_s{*}")
		dio_bp1, dio_err1 := queryMetric("avg:dummy.io.disk_bps{*}")
		dio_mb1, dio_err2 := queryMetric("avg:dummy.io.disk_mb_s{*}")
		// older app-level raw delta gauges
		dr1, err1 := queryMetric("avg:dummy.io.disk.read_bytes{*}")
		dw1, err2 := queryMetric("avg:dummy.io.disk.write_bytes{*}")
		// host-level raw gauges as a further fallback
		dr2, err3 := queryMetric("avg:system.disk.read_bytes{*}")
		dw2, err4 := queryMetric("avg:system.disk.write_bytes{*}")
		var acc float64 = 0
		found := false
		// prefer explicit disk_bps if present
		if derr1 == nil {
			diskBps = db1
			found = true
		} else if derr2 == nil {
			// disk_mb_s -> convert to bytes/sec
			diskBps = dm1 * 1000000.0
			found = true
		} else if dio_err1 == nil {
			diskBps = dio_bp1
			found = true
		} else if dio_err2 == nil {
			// dummy.io.disk_mb_s -> convert to bytes/sec
			diskBps = dio_mb1 * 1000000.0
			found = true
		} else {
			if err1 == nil {
				acc += dr1
				found = true
			}
			if err2 == nil {
				acc += dw1
				found = true
			}
			if err3 == nil {
				acc += dr2
				found = true
			}
			if err4 == nil {
				acc += dw2
				found = true
			}
			if found {
				diskBps = acc
			}
		}
	}
	if netBps == 0 {
		// try app-level network metrics (explicit rate gauges first)
		nb1, nerrb1 := queryMetric("avg:network_bps{*}")
		ng1, nerrg1 := queryMetric("avg:network_gbps{*}")
		dio_nb1, dio_nerr1 := queryMetric("avg:dummy.io.network_bps{*}")
		dio_ng1, dio_nerr2 := queryMetric("avg:dummy.io.network_gbps{*}")
		// older app-level raw delta gauges
		nr1, nerr1 := queryMetric("avg:dummy.io.net.bytes_recv{*}")
		ns1, nerr2 := queryMetric("avg:dummy.io.net.bytes_sent{*}")
		nr2, nerr3 := queryMetric("avg:system.net.bytes_recv{*}")
		ns2, nerr4 := queryMetric("avg:system.net.bytes_sent{*}")
		var accn float64 = 0
		foundn := false
		if nerrb1 == nil {
			netBps = nb1
			foundn = true
		} else if nerrg1 == nil {
			// network_gbps -> convert to bytes/sec (Gbps -> bps -> bytes/sec)
			netBps = (ng1 * 1e9) / 8.0
			foundn = true
		} else if dio_nerr1 == nil {
			netBps = dio_nb1
			foundn = true
		} else if dio_nerr2 == nil {
			// dummy.io.network_gbps -> convert
			netBps = (dio_ng1 * 1e9) / 8.0
			foundn = true
		} else {
			if nerr1 == nil {
				accn += nr1
				foundn = true
			}
			if nerr2 == nil {
				accn += ns1
				foundn = true
			}
			if nerr3 == nil {
				accn += nr2
				foundn = true
			}
			if nerr4 == nil {
				accn += ns2
				foundn = true
			}
			if foundn {
				netBps = accn
			}
		}
	}

	// Attempt to query Datadog Logs API (v2) for recent logs (best-effort)
	logs := make([]map[string]interface{}, 0)
	logsURL := fmt.Sprintf("https://%s/api/v2/logs/events/search", site)
	logsFilter := map[string]interface{}{
		"filter": map[string]string{
			"from": time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
			"to":   time.Now().Format(time.RFC3339),
		},
		"page": map[string]int{"limit": 20},
	}
	if jb, jerr := json.Marshal(logsFilter); jerr == nil {
		req, _ := http.NewRequestWithContext(ctx, "POST", logsURL, bytes.NewBuffer(jb))
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("DD-API-KEY", apiKey)
		}
		if appKey != "" {
			req.Header.Set("DD-APPLICATION-KEY", appKey)
		}
		lresp, lerr := s.client.Do(req)
		if lerr == nil {
			defer lresp.Body.Close()
			if lresp.StatusCode == 200 {
				var lb map[string]interface{}
				if err := json.NewDecoder(lresp.Body).Decode(&lb); err == nil {
					if dataArr, ok := lb["data"].([]interface{}); ok {
						for _, item := range dataArr {
							if it, ok := item.(map[string]interface{}); ok {
								attrs, _ := it["attributes"].(map[string]interface{})
								ts := ""
								if tval, ok := attrs["timestamp"].(string); ok {
									ts = tval
								}
								if tval, ok := attrs["@timestamp"].(string); ok && ts == "" {
									ts = tval
								}
								msg := ""
								if m, ok := attrs["message"].(string); ok {
									msg = m
								}
								if m2, ok := attrs["content"].(string); ok && msg == "" {
									msg = m2
								}
								svc := ""
								if s2, ok := attrs["service"].(string); ok {
									svc = s2
								}
								lvl := "INFO"
								if sev, ok := attrs["status"].(string); ok {
									lvl = sev
								}
								logs = append(logs, map[string]interface{}{"time": ts, "level": lvl, "message": msg, "service": svc})
							}
						}
					}
				}
			}
		}
	}

	// if logs found, prefer them as recentLogs
	if len(logs) > 0 {
		recentLogs = logs
	}

	// Try to query system uptime (seconds since boot) if available
	uptimeSeconds := float64(0)
	if us, uerr := queryMetric("last:system.uptime{*}"); uerr == nil {
		uptimeSeconds = us
	}

	// normalize / cap percents and convert bytes/sec to human-friendly units
	if cpuVal < 0 {
		cpuVal = 0
	}
	if cpuVal > 100 {
		cpuVal = 100
	}
	if memPct < 0 {
		memPct = 0
	}
	if memPct > 100 {
		memPct = 100
	}

	// convert disk bytes/sec to MB/s
	var diskMBs float64 = 0
	if diskBps > 0 {
		diskMBs = diskBps / (1024.0 * 1024.0)
	}
	// convert network bytes/sec to Gbps (bytes->bits, then /1e9)
	var networkGbps float64 = 0
	if netBps > 0 {
		networkGbps = (netBps * 8.0) / 1e9
	}

	// also compute network in Kbps for frontend convenience (kilobits/sec)
	var networkKbps float64 = 0
	if netBps > 0 {
		networkKbps = (netBps * 8.0) / 1000.0
	}

	metrics := map[string]interface{}{
		"active_incidents":      len(events) / 4,
		"resolved_today":        len(events) / 3,
		"avg_response_time":     "12m",
		"system_uptime":         "99.8%",
		"system_uptime_seconds": uptimeSeconds,
		"system_uptime_days": func() float64 {
			if uptimeSeconds > 0 {
				return uptimeSeconds / 86400.0
			}
			return 0
		}(),
		// numeric percent values
		"cpu_usage":        cpuVal,
		"memory_usage_pct": memPct,
		// raw bps and converted friendly units
		"disk_bps":     diskBps,
		"disk_mb_s":    diskMBs,
		"network_bps":  netBps,
		"network_gbps": networkGbps,
		"network_kbps": networkKbps,
	}

	// attempt to fetch monitors for this org and include in cache/payload
	mons := []map[string]interface{}{}
	if mm, merr := s.FetchMonitorsForOrg(ctx, orgID); merr == nil {
		mons = mm
	}

	s.cacheMu.Lock()
	s.cache[orgID] = &CachedDatadog{
		LastUpdated:        time.Now(),
		Metrics:            metrics,
		Timeline:           timeline,
		RecentLogs:         recentLogs,
		StatusDistribution: statusDist,
		Monitors:           mons,
	}
	s.cacheMu.Unlock()

	// Broadcast poll update (recent logs + metrics) to connected clients if broadcaster set
	if s.broadcaster != nil {
		// send a lightweight payload containing recent logs and latest metrics
		payload := map[string]interface{}{"type": "datadog_poll_update", "recent_logs": recentLogs, "metrics": metrics, "monitors": mons}
		go func() {
			defer func() { recover() }()
			s.broadcaster(orgID, payload)
		}()
	}
}

func (s *DatadogService) GetCachedForOrg(orgID string) *CachedDatadog {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	if c, ok := s.cache[orgID]; ok {
		// return shallow copy
		clone := &CachedDatadog{
			LastUpdated:        c.LastUpdated,
			Metrics:            make(map[string]interface{}),
			Timeline:           make([]map[string]interface{}, len(c.Timeline)),
			RecentLogs:         make([]map[string]interface{}, len(c.RecentLogs)),
			StatusDistribution: make([]map[string]interface{}, len(c.StatusDistribution)),
			Monitors:           make([]map[string]interface{}, len(c.Monitors)),
		}
		for k, v := range c.Metrics {
			clone.Metrics[k] = v
		}
		copy(clone.Timeline, c.Timeline)
		copy(clone.RecentLogs, c.RecentLogs)
		copy(clone.StatusDistribution, c.StatusDistribution)
		copy(clone.Monitors, c.Monitors)
		return clone
	}
	return nil
}

// ForceFetchOrg triggers an immediate background fetch for the given org.
// This is safe to call repeatedly; it simply schedules a fetch in a goroutine.
func (s *DatadogService) ForceFetchOrg(orgID string) {
	if orgID == "" {
		return
	}
	go func() {
		defer func() { recover() }()
		s.fetchForOrg(context.Background(), orgID)
	}()
}

// SetBroadcaster sets an optional callback that will be invoked when DatadogService
// wants to broadcast updates (e.g., new logs) to connected clients.
func (s *DatadogService) SetBroadcaster(fn func(orgID string, payload interface{})) {
	s.pollMu.Lock()
	defer s.pollMu.Unlock()
	s.broadcaster = fn
}

// AppendRecentLog appends a log entry to the in-memory recent logs for an org and trims history.
func (s *DatadogService) AppendRecentLog(orgID string, entry map[string]interface{}) {
	if orgID == "" || entry == nil {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	c, ok := s.cache[orgID]
	if !ok || c == nil {
		c = &CachedDatadog{
			LastUpdated:        time.Now(),
			Metrics:            make(map[string]interface{}),
			Timeline:           []map[string]interface{}{},
			RecentLogs:         []map[string]interface{}{},
			StatusDistribution: []map[string]interface{}{},
		}
		s.cache[orgID] = c
	}
	// prepend newest first
	c.RecentLogs = append([]map[string]interface{}{entry}, c.RecentLogs...)
	// cap to 100 entries
	if len(c.RecentLogs) > 100 {
		c.RecentLogs = c.RecentLogs[:100]
	}
	c.LastUpdated = time.Now()
}

// FetchMonitorsForOrg queries the Datadog Monitors API for the given org and caches the result.
func (s *DatadogService) FetchMonitorsForOrg(ctx context.Context, orgID string) ([]map[string]interface{}, error) {
	// Read organization doc to obtain datadog_secrets (reuse logic from fetchForOrg)
	docSnap, err := s.fs.Firestore.Collection("organizations").Doc(orgID).Get(ctx)
	if err != nil {
		return nil, err
	}
	data := docSnap.Data()

	var encAPI, encApp, wrappedDEK string
	if v, ok := data["datadog_secrets"].(map[string]interface{}); ok {
		if x, ok2 := v["encrypted_apiKey"].(string); ok2 {
			encAPI = x
		}
		if x, ok2 := v["encrypted_appKey"].(string); ok2 {
			encApp = x
		}
		if x, ok2 := v["encrypted_apiKeyDek"].(string); ok2 {
			wrappedDEK = x
		}
		if encAPI == "" {
			if x, ok2 := v["apiKey"].(string); ok2 {
				encAPI = x
			}
		}
		if encApp == "" {
			if x, ok2 := v["appKey"].(string); ok2 {
				encApp = x
			}
		}
	}

	if encAPI == "" {
		if x, ok := data["datadog_secrets.apiKey"].(string); ok {
			encAPI = x
		}
	}
	if encApp == "" {
		if x, ok := data["datadog_secrets.appKey"].(string); ok {
			encApp = x
		}
	}

	apiKey := ""
	appKey := ""
	if wrappedDEK != "" || encAPI != "" || encApp != "" {
		a, b, derr := DecryptData(encAPI, encApp, wrappedDEK)
		if derr == nil {
			apiKey = a
			appKey = b
		} else {
			if encAPI != "" {
				apiKey = encAPI
			}
			if encApp != "" {
				appKey = encApp
			}
		}
	}

	if apiKey == "" && appKey == "" {
		return nil, fmt.Errorf("no datadog credentials")
	}

	site := s.cfg.DataDog_SITE
	if site == "" {
		site = "api.datadoghq.com"
	}
	// list monitors (include all states)
	url := fmt.Sprintf("https://%s/api/v1/monitor?group_states=all", site)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if apiKey != "" {
		req.Header.Set("DD-API-KEY", apiKey)
	}
	if appKey != "" {
		req.Header.Set("DD-APPLICATION-KEY", appKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("datadog monitors status %d", resp.StatusCode)
	}

	var mons []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&mons); err != nil {
		return nil, err
	}

	// cache monitors
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	c, ok := s.cache[orgID]
	if !ok || c == nil {
		c = &CachedDatadog{
			LastUpdated:        time.Now(),
			Metrics:            make(map[string]interface{}),
			Timeline:           []map[string]interface{}{},
			RecentLogs:         []map[string]interface{}{},
			StatusDistribution: []map[string]interface{}{},
			Monitors:           []map[string]interface{}{},
		}
		s.cache[orgID] = c
	}
	c.Monitors = mons
	c.LastUpdated = time.Now()

	return mons, nil
}
