# Prometheus Alert Rules for Turbopuffer

Pre-configured Prometheus alert rules for monitoring Turbopuffer namespace health and detecting issues before they impact production.

## Alert Groups

### 1. Namespace Alerts (`turbopuffer_namespace_alerts`)

**TurbopufferHighUnindexedBytes** (Warning)
- **Trigger**: Namespace has >2GB (2,147,483,648 bytes) unindexed data for 5+ minutes
- **Impact**: May cause inconsistent query results
- **Severity**: Warning
- **Action**: Monitor indexing progress; investigate high write rates

**TurbopufferCriticalUnindexedBytes** (Critical)
- **Trigger**: Namespace has >5GB (5,368,709,120 bytes) unindexed data for 10+ minutes
- **Impact**: WILL cause inconsistent query results
- **Severity**: Critical
- **Action**: Immediate investigation required; consider pausing writes

**TurbopufferIndexingStuck** (Warning)
- **Trigger**: Namespace has >1GB unindexed with `index_status="updating"` for 30+ minutes
- **Impact**: Indicates slow or stuck indexing
- **Severity**: Warning
- **Action**: Monitor progress; contact support if not decreasing

### 2. Infrastructure Health Alerts (`turbopuffer_health_alerts`)

**TurbopufferLowIndexHealth** (Warning)
- **Trigger**: <95% of namespaces have up-to-date indexes for 15+ minutes
- **Impact**: Widespread indexing issues across infrastructure
- **Severity**: Warning
- **Action**: Review all namespaces; check for systemic issues

**TurbopufferMultipleNamespacesRequireAttention** (Warning)
- **Trigger**: 3+ namespaces have >2GB unindexed data for 10+ minutes
- **Impact**: Possible systemic issue (high writes, service degradation)
- **Severity**: Warning
- **Action**: Review dashboard for patterns; check Turbopuffer status

### 3. Exporter Health Alerts (`turbopuffer_exporter_alerts`)

**TurbopufferExporterDown** (Critical)
- **Trigger**: Exporter is unreachable for 5+ minutes
- **Impact**: No metrics collected; monitoring blind
- **Severity**: Critical
- **Action**: Check exporter status, logs, and API key

**TurbopufferExporterSlowScrapes** (Warning)
- **Trigger**: Scrapes take >30 seconds for 10+ minutes
- **Impact**: Delayed metrics; possible API issues
- **Severity**: Warning
- **Action**: Check API status; consider timeout tuning

**TurbopufferExporterStale** (Warning)
- **Trigger**: No successful scrape in 5+ minutes
- **Impact**: Stale metrics
- **Severity**: Warning
- **Action**: Check exporter logs and API connectivity

## Usage

### Option 1: Docker Compose (Recommended)

Mount the alert rules when running Prometheus:

```yaml
services:
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - ./prometheus/rules:/etc/prometheus/rules
      - prometheus_data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
    restart: unless-stopped
```

Update your `prometheus.yml` to load the rules:

```yaml
rule_files:
  - '/etc/prometheus/rules/*.yml'

alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - 'alertmanager:9093'  # Adjust to your Alertmanager address
```

### Option 2: Manual Installation

1. Copy the rules file to your Prometheus rules directory:
```bash
sudo cp prometheus/rules/turbopuffer.yml /etc/prometheus/rules/
```

2. Update `/etc/prometheus/prometheus.yml`:
```yaml
rule_files:
  - '/etc/prometheus/rules/*.yml'
```

3. Reload Prometheus:
```bash
# Using systemd
sudo systemctl reload prometheus

# Or send SIGHUP
kill -HUP $(pidof prometheus)
```

4. Verify rules are loaded:
```bash
# Check via Prometheus UI
open http://localhost:9090/rules

# Or via API
curl http://localhost:9090/api/v1/rules | jq '.data.groups[].name'
```

### Option 3: Kubernetes

Create a ConfigMap for the alert rules:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: turbopuffer-alerts
  namespace: monitoring
data:
  turbopuffer.yml: |
    # Paste contents of prometheus/rules/turbopuffer.yml here
```

Mount in Prometheus:

```yaml
volumeMounts:
  - name: turbopuffer-alerts
    mountPath: /etc/prometheus/rules/turbopuffer.yml
    subPath: turbopuffer.yml
volumes:
  - name: turbopuffer-alerts
    configMap:
      name: turbopuffer-alerts
```

## Alertmanager Configuration

To receive alerts, configure Alertmanager. Example `alertmanager.yml`:

### Email Notifications

```yaml
global:
  smtp_smarthost: 'smtp.gmail.com:587'
  smtp_from: 'alerts@example.com'
  smtp_auth_username: 'alerts@example.com'
  smtp_auth_password: 'your-app-password'

route:
  group_by: ['alertname', 'namespace']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  receiver: 'turbopuffer-team'
  routes:
    - match:
        severity: critical
      receiver: 'turbopuffer-critical'
      repeat_interval: 1h

receivers:
  - name: 'turbopuffer-team'
    email_configs:
      - to: 'team@example.com'
        headers:
          Subject: '[Turbopuffer] {{ .GroupLabels.alertname }}'

  - name: 'turbopuffer-critical'
    email_configs:
      - to: 'oncall@example.com'
        headers:
          Subject: '[CRITICAL] [Turbopuffer] {{ .GroupLabels.alertname }}'
```

### Slack Notifications

```yaml
receivers:
  - name: 'turbopuffer-team'
    slack_configs:
      - api_url: 'https://hooks.slack.com/services/YOUR/WEBHOOK/URL'
        channel: '#turbopuffer-alerts'
        title: '{{ .GroupLabels.alertname }}'
        text: |
          {{ range .Alerts }}
          *Alert:* {{ .Labels.alertname }}
          *Severity:* {{ .Labels.severity }}
          *Namespace:* {{ .Labels.namespace }}
          *Region:* {{ .Labels.region }}
          *Description:* {{ .Annotations.description }}
          *Dashboard:* {{ .Annotations.dashboard_url }}
          {{ end }}
```

### PagerDuty Integration

```yaml
receivers:
  - name: 'turbopuffer-critical'
    pagerduty_configs:
      - routing_key: 'your-pagerduty-integration-key'
        severity: 'critical'
        description: '{{ .GroupLabels.alertname }}: {{ .Annotations.summary }}'
```

## Testing Alerts

### Manually Trigger an Alert

To test that alerts are working:

1. **Reduce the threshold** temporarily in `turbopuffer.yml`:
```yaml
# Change from 2GB to 1MB for testing
- alert: TurbopufferHighUnindexedBytes
  expr: turbopuffer_namespace_unindexed_bytes > 1048576  # 1MB instead of 2GB
  for: 1m  # Shorter duration for testing
```

2. **Reload Prometheus**:
```bash
kill -HUP $(pidof prometheus)
```

3. **Wait for the alert to fire** (1-2 minutes based on `for` duration)

4. **Verify in Prometheus UI**:
```
http://localhost:9090/alerts
```

5. **Check Alertmanager**:
```
http://localhost:9093/#/alerts
```

6. **Restore original values** after testing

### Use PromQL to Check Alert Conditions

Before alerts fire, check if conditions are met:

```bash
# Check which namespaces would trigger warning
curl -s 'http://localhost:9090/api/v1/query?query=turbopuffer_namespace_unindexed_bytes>2147483648' | jq '.data.result[] | {namespace: .metric.namespace, value: .value[1]}'

# Check index health percentage
curl -s 'http://localhost:9090/api/v1/query?query=(count(turbopuffer_namespace_rows{index_status="up-to-date"})/count(turbopuffer_namespace_rows))*100' | jq '.data.result[0].value[1]'

# Check if exporter is up
curl -s 'http://localhost:9090/api/v1/query?query=up{job="turbopuffer"}' | jq '.data.result[0].value[1]'
```

## Customization

### Adjust Thresholds

Edit `prometheus/rules/turbopuffer.yml` to change thresholds:

```yaml
# Warning at 3GB instead of 2GB
- alert: TurbopufferHighUnindexedBytes
  expr: turbopuffer_namespace_unindexed_bytes > 3221225472  # 3GB in bytes

# Critical at 10GB instead of 5GB
- alert: TurbopufferCriticalUnindexedBytes
  expr: turbopuffer_namespace_unindexed_bytes > 10737418240  # 10GB in bytes
```

### Adjust Durations

Change the `for` duration to make alerts more or less sensitive:

```yaml
# Fire faster (less tolerant of spikes)
- alert: TurbopufferHighUnindexedBytes
  expr: turbopuffer_namespace_unindexed_bytes > 2147483648
  for: 2m  # Down from 5m

# Fire slower (more tolerant of transient issues)
- alert: TurbopufferCriticalUnindexedBytes
  expr: turbopuffer_namespace_unindexed_bytes > 5368709120
  for: 30m  # Up from 10m
```

### Add Custom Labels

Add labels for routing:

```yaml
- alert: TurbopufferHighUnindexedBytes
  expr: turbopuffer_namespace_unindexed_bytes > 2147483648
  for: 5m
  labels:
    severity: warning
    service: turbopuffer
    team: data-platform      # Custom label
    environment: production  # Custom label
```

### Namespace-Specific Alerts

Create alerts for specific important namespaces:

```yaml
- alert: TurbopufferProductionNamespaceUnindexed
  expr: turbopuffer_namespace_unindexed_bytes{namespace="production-vectors"} > 1073741824  # 1GB
  for: 5m
  labels:
    severity: critical
    service: turbopuffer
  annotations:
    summary: "Production namespace has unindexed data"
    description: "Critical production namespace has {{ $value | humanize1024 }}B unindexed"
```

## Alert Annotations

All alerts include helpful annotations:

- **summary**: Short description of the issue
- **description**: Detailed explanation with context and action items
- **dashboard_url**: Direct link to Grafana dashboard filtered to the affected namespace
- **runbook_url**: Link to documentation (update URLs for your environment)

### Customizing Dashboard URLs

Update the `dashboard_url` to match your Grafana deployment:

```yaml
annotations:
  dashboard_url: "https://grafana.yourcompany.com/d/turbopuffer-overview?var-namespace={{ $labels.namespace }}"
```

## Silence Alerts

To temporarily silence alerts (e.g., during maintenance):

### Via Alertmanager UI

1. Go to `http://localhost:9093/#/silences`
2. Click "New Silence"
3. Add matchers:
   - `alertname = TurbopufferHighUnindexedBytes`
   - `namespace = your-namespace`
4. Set duration and reason
5. Create

### Via API

```bash
# Silence all Turbopuffer alerts for 2 hours
curl -X POST http://localhost:9093/api/v1/silences \
  -H 'Content-Type: application/json' \
  -d '{
    "matchers": [{"name": "service", "value": "turbopuffer", "isRegex": false}],
    "startsAt": "'$(date -u +%Y-%m-%dT%H:%M:%S.000Z)'",
    "endsAt": "'$(date -u -d '+2 hours' +%Y-%m-%dT%H:%M:%S.000Z)'",
    "createdBy": "admin",
    "comment": "Planned maintenance"
  }'
```

## Monitoring the Alerts

### Check Alert Status

```bash
# List all active alerts
curl http://localhost:9090/api/v1/alerts | jq '.data.alerts[] | {alertname: .labels.alertname, state: .state, namespace: .labels.namespace}'

# Count alerts by severity
curl http://localhost:9090/api/v1/alerts | jq '.data.alerts | group_by(.labels.severity) | map({severity: .[0].labels.severity, count: length})'
```

### Grafana Alert Dashboard

The Turbopuffer Overview dashboard shows:
- "Namespaces Requiring Attention" panel (count triggering alerts)
- Color-coded thresholds matching alert conditions
- Direct links from panels to filtered views

## Troubleshooting

### Alerts Not Firing

1. **Check rule syntax**:
```bash
promtool check rules prometheus/rules/turbopuffer.yml
```

2. **Verify rules are loaded**:
```
http://localhost:9090/rules
```

3. **Test the PromQL expression** in Prometheus UI:
```
http://localhost:9090/graph
```

4. **Check evaluation logs**:
```bash
docker logs prometheus 2>&1 | grep -i "error\|warning"
```

### False Positives

If alerts fire too frequently:
- Increase `for` duration to tolerate transient spikes
- Adjust thresholds higher
- Add additional conditions to the expression

### Alerts Not Routing to Alertmanager

1. **Check Alertmanager is running**:
```bash
curl http://localhost:9093/api/v1/status
```

2. **Verify Prometheus config**:
```yaml
alerting:
  alertmanagers:
    - static_configs:
        - targets: ['localhost:9093']
```

3. **Check connectivity**:
```bash
curl http://localhost:9090/api/v1/alertmanagers
```

## Best Practices

1. **Start Conservative**: Use longer `for` durations initially to avoid alert fatigue
2. **Tune Over Time**: Adjust thresholds based on your actual usage patterns
3. **Document Runbooks**: Create runbook links for common resolution procedures
4. **Test Regularly**: Manually trigger alerts quarterly to verify the pipeline works
5. **Review Silences**: Don't leave silences active indefinitely; set short durations
6. **Monitor Alert Volume**: Track alert frequency to identify noisy rules
7. **Use Labels Wisely**: Add routing labels for team/environment but keep cardinality low

## Complete Docker Compose Example

See `DOCKER.md` for a full monitoring stack including Alertmanager.

## Support

For issues with:
- **Alert rules**: File an issue in the puff repository
- **Prometheus**: See [Prometheus documentation](https://prometheus.io/docs/alerting/latest/overview/)
- **Alertmanager**: See [Alertmanager documentation](https://prometheus.io/docs/alerting/latest/alertmanager/)
- **Turbopuffer**: Contact Turbopuffer support
