# Turbopuffer Grafana Dashboard

This directory contains a Grafana dashboard for monitoring Turbopuffer namespace metrics collected by the puff Prometheus exporter.

## Overview

The **Turbopuffer Overview** dashboard provides comprehensive monitoring of your Turbopuffer infrastructure with:

- **17 panels** across 7 rows showing namespace metrics, storage trends, and index health
- **Multi-select variables** for filtering by region and namespace
- **2GB threshold warnings** for unindexed bytes (configurable thresholds at 2GB and 5GB)
- **Aggregate and filtered views** - see totals across all namespaces or drill down to specific ones

### Dashboard Sections

1. **Overview Stats** (Row 1): Total namespaces, rows, storage, and unindexed bytes
2. **Alert Stats** (Row 2): Namespaces requiring attention, index health %, average lag, growth rate
3. **Time Series** (Rows 3-4): Row count, storage size, and unindexed bytes trends over time
4. **Table View** (Row 5): Detailed per-namespace breakdown with gradient gauge for unindexed bytes
5. **Distribution Charts** (Row 6): Regional distribution and encryption breakdown
6. **Exporter Health** (Row 7, collapsed): Troubleshooting panels for exporter performance

## Prerequisites

Before importing this dashboard, ensure you have:

1. **Grafana 9.x or 10.x** installed and running
2. **Prometheus** configured and scraping the puff exporter (see below)
3. **puff-exporter** running and exposing metrics on port 9876 (or your configured port)

### Prometheus Configuration

Add the puff exporter as a scrape target in your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'turbopuffer'
    scrape_interval: 60s
    scrape_timeout: 30s
    static_configs:
      - targets: ['localhost:9876']  # Adjust host/port as needed
```

After updating the config, reload Prometheus:
```bash
# If using systemd
sudo systemctl reload prometheus

# Or send SIGHUP
kill -HUP $(pidof prometheus)
```

Verify the exporter is being scraped:
```bash
# Check Prometheus targets
curl http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | select(.labels.job=="turbopuffer")'
```

## Importing the Dashboard

### Option 1: Manual Import via UI

1. Open Grafana in your browser (default: `http://localhost:3000`)
2. Navigate to **Dashboards** → **Import** (or press `+` → **Import**)
3. Click **Upload JSON file** and select `dashboards/turbopuffer-overview.json`
4. Select your Prometheus datasource from the dropdown
5. Click **Import**

### Option 2: Auto-Provisioning (Recommended for Production)

For automatic dashboard provisioning when Grafana starts:

1. Copy provisioning files to your Grafana configuration directory:
```bash
# Adjust paths for your Grafana installation
sudo cp provisioning/dashboards.yml /etc/grafana/provisioning/dashboards/
sudo mkdir -p /var/lib/grafana/dashboards
sudo cp dashboards/turbopuffer-overview.json /var/lib/grafana/dashboards/
```

2. Copy the datasource config (if not already configured):
```bash
sudo cp provisioning/datasources.yml /etc/grafana/provisioning/datasources/
```

3. Restart Grafana:
```bash
sudo systemctl restart grafana-server
```

The dashboard will automatically appear in Grafana under the "Turbopuffer" folder.

### Option 3: Docker Compose

See the root `DOCKER.md` file for a complete monitoring stack example with Prometheus, Grafana, and puff-exporter.

## Using the Dashboard

### Variables

The dashboard includes three variables at the top:

1. **Datasource** (`DS_PROMETHEUS`): Select your Prometheus datasource
   - Visible dropdown
   - Allows switching between multiple Prometheus instances

2. **Region** (multi-select): Filter by Turbopuffer region
   - Default: `All` (shows all regions)
   - Example regions: `aws-us-east-1`, `aws-eu-west-1`, etc.
   - Can select multiple regions to compare

3. **Namespace** (multi-select): Filter by namespace ID
   - Default: `All` (shows aggregate across all namespaces)
   - Depends on the region filter
   - Can select multiple namespaces to compare

### Viewing Aggregate Metrics

**Default behavior**: When both variables are set to "All", you see aggregate metrics across your entire Turbopuffer infrastructure:
- Total Namespaces: Count of all namespaces
- Total Rows: Sum of rows across all namespaces
- Total Storage: Sum of logical bytes across all namespaces
- Total Unindexed Bytes: Sum with color-coded thresholds

### Drilling Down to Specific Namespaces

To investigate a specific namespace:

1. Click the **Namespace** dropdown at the top
2. Search for or select your namespace ID
3. All panels will update to show only that namespace's metrics

To compare multiple namespaces:
1. Click the **Namespace** dropdown
2. Select multiple namespace IDs (they will appear in the dropdown)
3. Time series panels will show separate lines for each namespace

### Understanding the 2GB Threshold

The dashboard highlights when unindexed bytes exceed **2GB** (2,147,483,648 bytes):

- **Total Unindexed Bytes** panel: Turns yellow at 2GB, red at 5GB
- **Namespaces Requiring Attention**: Shows count of namespaces above 2GB threshold
- **Unindexed Bytes Over Time**: Yellow threshold line at 2GB
- **Table View**: Gradient gauge with color background (green < 2GB, yellow 2-5GB, red > 5GB)

**Why 2GB matters**: Large amounts of unindexed data indicate that Turbopuffer's indexing process is lagging behind data ingestion. This can impact query performance. When a namespace exceeds 2GB of unindexed data, it may require attention:
- Check if data ingestion rate is unusually high
- Monitor the namespace's index status (should be "up-to-date")
- Consider spreading writes across multiple namespaces if sustained

### Index Health Percentage

The **Index Health Percentage** gauge shows what percentage of your namespaces have `index_status="up-to-date"`:
- **Target**: >95% (green)
- **Warning**: 95-99% (yellow)
- **Critical**: <95% (red)

A healthy system should have most namespaces with up-to-date indexes.

## PromQL Query Reference

For users new to Prometheus, here are the key queries used in this dashboard:

### Aggregation Queries (with empty result handling)

```promql
# Total namespaces
count(turbopuffer_namespace_rows{region=~"$region", namespace=~"$namespace"})

# Total rows (returns 0 if no data)
sum(turbopuffer_namespace_rows{region=~"$region", namespace=~"$namespace"}) or vector(0)

# Total storage
sum(turbopuffer_namespace_logical_bytes{region=~"$region", namespace=~"$namespace"}) or vector(0)

# Total unindexed bytes
sum(turbopuffer_namespace_unindexed_bytes{region=~"$region", namespace=~"$namespace"}) or vector(0)
```

The `or vector(0)` ensures the query returns 0 instead of "no data" when there are no matching namespaces.

### Alert Queries

```promql
# Count of namespaces above 2GB threshold
count(turbopuffer_namespace_unindexed_bytes{region=~"$region", namespace=~"$namespace"} > 2147483648) or vector(0)

# Index health percentage
(count(turbopuffer_namespace_rows{region=~"$region", namespace=~"$namespace", index_status="up-to-date"}) / count(turbopuffer_namespace_rows{region=~"$region", namespace=~"$namespace"})) * 100 or vector(100)
```

### Growth Rate

```promql
# Storage growth rate (bytes per second over 1-hour window)
sum(rate(turbopuffer_namespace_logical_bytes{region=~"$region", namespace=~"$namespace"}[1h])) or vector(0)
```

The `rate()` function calculates the per-second average rate of increase.

### Distribution Queries

```promql
# Count namespaces per region
count by (region) (turbopuffer_namespace_rows{region=~"$region", namespace=~"$namespace"})

# Storage per region
sum by (region) (turbopuffer_namespace_logical_bytes{region=~"$region", namespace=~"$namespace"})
```

The `by (region)` groups results by the region label.

## Troubleshooting

### Problem: "No data" in all panels

**Possible causes:**

1. **Prometheus not scraping the exporter**
   - Check Prometheus targets: `http://localhost:9090/targets`
   - Look for the `turbopuffer` job
   - Verify target is "UP" and last scrape was recent

2. **Exporter not running**
   - Check if the puff exporter is running: `curl http://localhost:9876/health`
   - Should return JSON with `{"status": "ok"}`
   - If not, start the exporter: `TURBOPUFFER_API_KEY=your_key npm run dev -- export`

3. **Wrong datasource selected**
   - Check the **Datasource** dropdown at the top of the dashboard
   - Ensure it points to your Prometheus instance

4. **Firewall/network issues**
   - Verify Prometheus can reach the exporter: `curl http://<exporter-host>:9876/metrics`
   - Should return Prometheus metrics in text format

5. **No namespaces exist**
   - Check if you have any Turbopuffer namespaces: `puff list`
   - Create a test namespace if needed

**Troubleshooting steps:**

```bash
# 1. Check if exporter is running
curl http://localhost:9876/health

# 2. Verify metrics are being exposed
curl http://localhost:9876/metrics | grep turbopuffer_namespace

# 3. Check if Prometheus is scraping
curl http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | select(.labels.job=="turbopuffer")'

# 4. Query Prometheus directly
curl 'http://localhost:9090/api/v1/query?query=turbopuffer_namespace_rows'
```

### Problem: Variables show no values

**Symptom**: The Region and Namespace dropdowns are empty or show "No options found"

**Solutions:**

1. **Check if Prometheus has data**:
```bash
curl 'http://localhost:9090/api/v1/query?query=turbopuffer_namespace_rows' | jq
```
Should return results with `region` and `namespace` labels.

2. **Verify datasource variable**:
   - The `DS_PROMETHEUS` variable must be set first
   - Click the datasource dropdown and select your Prometheus instance

3. **Refresh variables**:
   - Click the refresh icon next to the variable dropdown
   - Or refresh the entire dashboard

4. **Check exporter configuration**:
   - Ensure the exporter is querying the correct Turbopuffer region
   - If using `--region` flag, verify the region is correct
   - Use `--all-regions` to query all regions

### Problem: Thresholds not showing colors

**Symptom**: The 2GB threshold is not highlighted in yellow

**Solutions:**

1. **Verify Grafana version compatibility**:
   - This dashboard targets Grafana 10.x (schema version 39)
   - Grafana 9.x should also work but may have minor visual differences
   - Grafana <9.x may not support all features

2. **Check panel configuration**:
   - Edit the panel (click title → Edit)
   - Go to **Field** tab → **Thresholds**
   - Verify thresholds are set:
     - Green: Base (0)
     - Yellow: 2147483648 (2GB)
     - Red: 5368709120 (5GB)

3. **Table cell coloring**:
   - In the table panel, thresholds use field overrides
   - Edit panel → **Overrides** tab → Look for "Value #C" (Unindexed Bytes)
   - Should have `custom.cellOptions` set to "color-background" with "gradient" mode

### Problem: Collapsed row doesn't expand

**Symptom**: The "Exporter Health (Troubleshooting)" row doesn't show panels when clicked

**Solution:**

1. Click directly on the row title "Exporter Health (Troubleshooting)"
2. The row should expand to show two panels: "Exporter Scrape Duration" and "Time Since Last Scrape"
3. If it still doesn't work, try refreshing the dashboard or re-importing the JSON

### Problem: Time series shows gaps

**Symptom**: Graphs have breaks or missing data points

**Solutions:**

1. **Check exporter refresh interval**:
   - Default is 60 seconds
   - Increase the interval if API calls are timing out: `--interval 120`

2. **Verify Prometheus scrape interval**:
   - Should match or be slightly longer than exporter refresh interval
   - Check `prometheus.yml`: `scrape_interval: 60s`

3. **Check for exporter errors**:
   - Look at exporter logs for API errors
   - Expand the "Exporter Health" collapsed row and check "Time Since Last Scrape"
   - Should be <120 seconds (green)

## Optional: Prometheus Alertmanager Integration

You can create alerts based on these metrics. Example alert rules:

```yaml
# /etc/prometheus/rules/turbopuffer.yml
groups:
  - name: turbopuffer_alerts
    interval: 60s
    rules:
      - alert: TurbopufferHighUnindexedBytes
        expr: turbopuffer_namespace_unindexed_bytes > 2147483648
        for: 5m
        labels:
          severity: warning
          service: turbopuffer
        annotations:
          summary: "Namespace {{ $labels.namespace }} has high unindexed bytes"
          description: "Namespace {{ $labels.namespace }} in region {{ $labels.region }} has {{ $value | humanize1024 }}B of unindexed data (threshold: 2GB)"

      - alert: TurbopufferCriticalUnindexedBytes
        expr: turbopuffer_namespace_unindexed_bytes > 5368709120
        for: 10m
        labels:
          severity: critical
          service: turbopuffer
        annotations:
          summary: "Namespace {{ $labels.namespace }} has critically high unindexed bytes"
          description: "Namespace {{ $labels.namespace }} in region {{ $labels.region }} has {{ $value | humanize1024 }}B of unindexed data (critical threshold: 5GB). Immediate attention required."

      - alert: TurbopufferIndexHealthLow
        expr: (count(turbopuffer_namespace_rows{index_status="up-to-date"}) / count(turbopuffer_namespace_rows)) * 100 < 95
        for: 15m
        labels:
          severity: warning
          service: turbopuffer
        annotations:
          summary: "Turbopuffer index health is below 95%"
          description: "Only {{ $value | printf \"%.1f\" }}% of namespaces have up-to-date indexes. Target is >95%."

      - alert: TurbopufferExporterDown
        expr: up{job="turbopuffer"} == 0
        for: 5m
        labels:
          severity: critical
          service: turbopuffer
        annotations:
          summary: "Turbopuffer exporter is down"
          description: "The puff Prometheus exporter has been down for more than 5 minutes."
```

Load the rules in Prometheus:

```yaml
# prometheus.yml
rule_files:
  - '/etc/prometheus/rules/*.yml'
```

Then reload Prometheus to activate alerts.

## Dashboard Customization

### Changing Thresholds

To adjust the 2GB threshold to a different value:

1. Edit the dashboard (click gear icon → **Settings** → **JSON Model**)
2. Search for `2147483648` (2GB in bytes)
3. Replace with your desired threshold in bytes
   - 1GB = 1073741824
   - 2GB = 2147483648
   - 5GB = 5368709120
   - 10GB = 10737418240
4. Save the updated JSON

Alternatively, edit individual panels (click panel title → Edit → Field tab → Thresholds).

### Adding More Panels

To add custom panels:

1. Click **Add** → **Visualization** at the top of the dashboard
2. Select the datasource (Prometheus)
3. Write your PromQL query using the available metrics:
   - `turbopuffer_namespace_rows`
   - `turbopuffer_namespace_logical_bytes`
   - `turbopuffer_namespace_unindexed_bytes`
   - `turbopuffer_namespace_info`
   - `turbopuffer_exporter_scrape_duration_seconds`
   - `turbopuffer_exporter_last_scrape_timestamp_seconds`
4. Configure visualization, thresholds, and panel options
5. Save the panel and dashboard

### Exporting Your Customizations

After making changes:

1. Go to **Dashboard settings** (gear icon) → **JSON Model**
2. Copy the JSON
3. Save to `dashboards/turbopuffer-overview-custom.json`
4. Commit to version control

## Metrics Reference

### Available Metrics

All metrics from the puff exporter:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `turbopuffer_namespace_rows` | Gauge | namespace, region, encryption, index_status | Approximate number of rows in namespace |
| `turbopuffer_namespace_logical_bytes` | Gauge | namespace, region, encryption, index_status | Approximate logical storage size in bytes |
| `turbopuffer_namespace_unindexed_bytes` | Gauge | namespace, region, encryption, index_status | Number of unindexed bytes (0 when up-to-date) |
| `turbopuffer_namespace_info` | Gauge | namespace, region, encryption, index_status, updated_at | Namespace metadata (value always 1) |
| `turbopuffer_exporter_scrape_duration_seconds` | Gauge | - | Time taken to fetch metrics from Turbopuffer API |
| `turbopuffer_exporter_last_scrape_timestamp_seconds` | Gauge | - | Unix timestamp of last successful scrape |

### Label Values

- **namespace**: Namespace ID (e.g., `my-vectors`, `prod-embeddings`)
- **region**: Turbopuffer region (e.g., `aws-us-east-1`, `aws-eu-west-1`)
- **encryption**: Encryption type (`sse` or `cmek`)
- **index_status**: Index status (`up-to-date` or `updating`)
- **updated_at**: ISO 8601 timestamp of last namespace update (only on `namespace_info`)

## Support

For issues with:
- **This dashboard**: Open an issue in the puff repository
- **The puff exporter**: See `DOCKER.md` and the main README
- **Turbopuffer service**: Contact Turbopuffer support
- **Grafana**: See [Grafana documentation](https://grafana.com/docs/)
- **Prometheus**: See [Prometheus documentation](https://prometheus.io/docs/)

## License

This dashboard is part of the puff CLI project. See the main repository for license information.
