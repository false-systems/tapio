use std::collections::HashMap;
use std::io::{self, Write};
use std::path::{Path, PathBuf};

use clap::{Parser, Subcommand};
use tapio_common::occurrence::{Occurrence, Severity};

#[derive(Parser)]
#[command(name = "tapio", version, about = "Kernel eyes for Kubernetes")]
struct Cli {
    /// Data directory for file sink output
    #[arg(long, env = "TAPIO_DATA_DIR", default_value = ".tapio/occurrences")]
    data_dir: PathBuf,

    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Show observer status and event rates
    Status,
    /// Live stream of kernel anomalies
    Watch {
        /// Output raw JSON
        #[arg(long)]
        json: bool,
        /// Filter: network observer only
        #[arg(long)]
        network: bool,
        /// Filter: container observer only
        #[arg(long)]
        container: bool,
        /// Filter: storage observer only
        #[arg(long)]
        storage: bool,
        /// Filter: node observer only
        #[arg(long)]
        node: bool,
    },
    /// Recent anomalies on this node
    Recent {
        /// Output raw JSON
        #[arg(long)]
        json: bool,
        /// Time window (e.g. 5m, 1h, 30s)
        #[arg(long)]
        since: Option<String>,
    },
    /// Node health summary
    Health {
        /// Observer to drill into (network, container, storage, node)
        observer: Option<String>,
    },
    /// Start MCP server (stdio transport)
    Mcp,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();

    match cli.command {
        Command::Status => cmd_status(&cli.data_dir)?,
        Command::Watch {
            json,
            network,
            container,
            storage,
            node,
        } => {
            let mut filters = Vec::new();
            if network {
                filters.push("network");
            }
            if container {
                filters.push("container");
            }
            if storage {
                filters.push("storage");
            }
            if node {
                filters.push("node");
            }
            cmd_watch(&cli.data_dir, json, &filters).await?;
        }
        Command::Recent { json, since } => cmd_recent(&cli.data_dir, json, since.as_deref())?,
        Command::Health { observer } => cmd_health(&cli.data_dir, observer.as_deref())?,
        Command::Mcp => cmd_mcp(&cli.data_dir).await?,
    }

    Ok(())
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

fn cmd_status(data_dir: &Path) -> anyhow::Result<()> {
    let occurrences = load_occurrences(data_dir)?;
    let mut out = io::stdout().lock();

    if occurrences.is_empty() {
        writeln!(out, "no anomalies recorded")?;
        return Ok(());
    }

    let mut by_observer: HashMap<&str, Vec<&Occurrence>> = HashMap::new();
    for occ in &occurrences {
        by_observer
            .entry(observer_category(&occ.occurrence_type))
            .or_default()
            .push(occ);
    }

    writeln!(out, "Observer     Count  Last Seen")?;
    writeln!(out, "─────────    ─────  ─────────")?;

    let mut observers: Vec<_> = by_observer.iter().collect();
    observers.sort_by_key(|(name, _)| *name);

    for (name, occs) in &observers {
        let last = occs
            .iter()
            .map(|o| o.timestamp.as_str())
            .max()
            .unwrap_or("-");
        writeln!(
            out,
            "{:<12} {:<5}  {}",
            name,
            occs.len(),
            format_timestamp(last)
        )?;
    }

    writeln!(out, "─────────    ─────")?;
    writeln!(out, "total        {}", occurrences.len())?;

    Ok(())
}

// ---------------------------------------------------------------------------
// Watch
// ---------------------------------------------------------------------------

async fn cmd_watch(data_dir: &Path, json: bool, filters: &[&str]) -> anyhow::Result<()> {
    use std::collections::HashSet;
    use std::ffi::OsString;
    use tokio::time::Duration;

    let mut seen: HashSet<OsString> = HashSet::new();

    // Mark existing files as already seen
    if data_dir.exists() {
        for entry in std::fs::read_dir(data_dir)? {
            seen.insert(entry?.file_name());
        }
    }

    loop {
        tokio::select! {
            _ = tokio::signal::ctrl_c() => break,
            _ = tokio::time::sleep(Duration::from_millis(200)) => {
                if !data_dir.exists() { continue; }
                let entries = match std::fs::read_dir(data_dir) {
                    Ok(e) => e,
                    Err(e) => {
                        eprintln!("warning: failed to read {}: {e}", data_dir.display());
                        continue;
                    }
                };
                let mut new_occs = Vec::new();
                for entry in entries {
                    let Ok(entry) = entry else { continue };
                    if !seen.insert(entry.file_name()) { continue; }
                    let path = entry.path();
                    if path.extension().is_some_and(|e| e == "json")
                        && let Ok(data) = std::fs::read_to_string(&path)
                        && let Ok(occ) = serde_json::from_str::<Occurrence>(&data)
                    {
                        new_occs.push(occ);
                    }
                }
                new_occs.sort_by(|a, b| a.timestamp.cmp(&b.timestamp));
                let mut out = io::stdout().lock();
                for occ in &new_occs {
                    if matches_filter(occ, filters) {
                        write_occurrence(&mut out, occ, json)?;
                    }
                }
                out.flush()?;
            }
        }
    }
    Ok(())
}

// ---------------------------------------------------------------------------
// Recent
// ---------------------------------------------------------------------------

fn cmd_recent(data_dir: &Path, json: bool, since: Option<&str>) -> anyhow::Result<()> {
    let mut occurrences = load_occurrences(data_dir)?;
    let mut out = io::stdout().lock();

    if occurrences.is_empty() {
        writeln!(out, "no anomalies recorded")?;
        return Ok(());
    }

    // Filter by time window
    if let Some(since_str) = since {
        let duration = parse_duration(since_str)?;
        let cutoff = chrono::Utc::now() - duration;
        let cutoff_str = cutoff.to_rfc3339();
        occurrences.retain(|o| o.timestamp >= cutoff_str);
    }

    // Sort descending by timestamp
    occurrences.sort_by(|a, b| b.timestamp.cmp(&a.timestamp));

    // Show last 20
    for occ in occurrences.iter().take(20) {
        write_occurrence(&mut out, occ, json)?;
    }
    out.flush()?;
    Ok(())
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

fn cmd_health(data_dir: &Path, observer_filter: Option<&str>) -> anyhow::Result<()> {
    let occurrences = load_occurrences(data_dir)?;
    let mut out = io::stdout().lock();

    // Filter to last hour for rate assessment
    let one_hour_ago = (chrono::Utc::now() - chrono::Duration::hours(1)).to_rfc3339();
    let recent: Vec<&Occurrence> = occurrences
        .iter()
        .filter(|o| o.timestamp >= one_hour_ago)
        .collect();

    struct ObserverStats<'a> {
        count: usize,
        critical_count: usize,
        last_seen: &'a str,
        type_counts: HashMap<&'a str, usize>,
    }

    let mut stats: HashMap<&str, ObserverStats> = HashMap::new();

    for occ in &recent {
        let cat = observer_category(&occ.occurrence_type);
        let entry = stats.entry(cat).or_insert_with(|| ObserverStats {
            count: 0,
            critical_count: 0,
            last_seen: "",
            type_counts: HashMap::new(),
        });
        entry.count += 1;
        if matches!(occ.severity, Severity::Critical) {
            entry.critical_count += 1;
        }
        if occ.timestamp.as_str() > entry.last_seen {
            entry.last_seen = &occ.timestamp;
        }
        *entry
            .type_counts
            .entry(occ.occurrence_type.as_str())
            .or_insert(0) += 1;
    }

    let categories = ["network", "container", "storage", "node"];

    if let Some(filter) = observer_filter {
        // Drill into one observer
        let s = stats.get(filter);
        writeln!(out, "Observer: {filter}")?;
        writeln!(out)?;
        match s {
            None => writeln!(out, "  no anomalies in last hour")?,
            Some(s) => {
                writeln!(out, "  anomalies:  {}", s.count)?;
                writeln!(out, "  critical:   {}", s.critical_count)?;
                writeln!(out, "  last seen:  {}", format_timestamp(s.last_seen))?;
                writeln!(out)?;
                let mut types: Vec<_> = s.type_counts.iter().collect();
                types.sort_by(|a, b| b.1.cmp(a.1));
                for (t, c) in types {
                    writeln!(out, "  {t}  ({c})")?;
                }
            }
        }
    } else {
        // Summary of all observers
        writeln!(
            out,
            "Observer     Anomalies  Last Seen            Most Frequent"
        )?;
        writeln!(
            out,
            "─────────    ─────────  ─────────            ─────────────"
        )?;

        let mut total_count = 0;
        let mut total_critical = 0;

        for cat in &categories {
            let (count, critical, last, most_freq) = match stats.get(cat) {
                Some(s) => {
                    let most = s
                        .type_counts
                        .iter()
                        .max_by_key(|(_, c)| *c)
                        .map(|(t, c)| format!("{t} ({c})"))
                        .unwrap_or_default();
                    (
                        s.count,
                        s.critical_count,
                        format_timestamp(s.last_seen),
                        most,
                    )
                }
                None => (0, 0, "-".to_string(), "-".to_string()),
            };
            total_count += count;
            total_critical += critical;
            writeln!(out, "{:<12} {:<9}  {:<20} {}", cat, count, last, most_freq)?;
        }

        writeln!(out)?;
        let health = health_status(total_count, total_critical);
        writeln!(
            out,
            "overall: {health} ({total_count} anomalies, {total_critical} critical in last hour)"
        )?;
    }
    Ok(())
}

// ---------------------------------------------------------------------------
// MCP Server (stdio transport, JSON-RPC 2.0)
// ---------------------------------------------------------------------------

async fn cmd_mcp(data_dir: &Path) -> anyhow::Result<()> {
    use std::io::BufRead;

    let stdin = io::stdin();
    let reader = stdin.lock();
    let mut line = String::new();
    let mut reader = io::BufReader::new(reader);

    loop {
        line.clear();
        let n = reader.read_line(&mut line)?;
        if n == 0 {
            break; // EOF
        }

        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }

        let request: serde_json::Value = match serde_json::from_str(trimmed) {
            Ok(v) => v,
            Err(_) => {
                mcp_write_error(None, -32700, "Parse error")?;
                continue;
            }
        };

        // JSON-RPC 2.0: missing "id" field = notification (no response).
        // Present "id" field (even if null) = request (must respond).
        let has_id = request.get("id").is_some();
        let id = request.get("id").cloned();
        let method = request.get("method").and_then(|m| m.as_str()).unwrap_or("");
        let params = request
            .get("params")
            .cloned()
            .unwrap_or(serde_json::json!({}));

        if !has_id {
            continue; // notification — no response
        }

        match method {
            "initialize" => {
                let result = serde_json::json!({
                    "protocolVersion": "2024-11-05",
                    "capabilities": { "tools": {} },
                    "serverInfo": {
                        "name": "tapio",
                        "version": env!("CARGO_PKG_VERSION"),
                    }
                });
                mcp_write_result(id, result)?;
            }
            "tools/list" => {
                let tools = serde_json::json!({
                    "tools": [
                        {
                            "name": "tapio_recent_anomalies",
                            "description": "Get recent kernel anomalies from the last N minutes",
                            "inputSchema": {
                                "type": "object",
                                "properties": {
                                    "minutes": { "type": "number", "default": 5, "description": "Time window in minutes" },
                                    "observer": { "type": "string", "enum": ["network", "container", "storage", "node", "all"], "default": "all" },
                                    "severity": { "type": "string", "enum": ["warning", "error", "critical", "all"], "default": "all" }
                                }
                            }
                        },
                        {
                            "name": "tapio_node_health",
                            "description": "Get current node health summary across all observers",
                            "inputSchema": { "type": "object", "properties": {} }
                        },
                        {
                            "name": "tapio_watch_stream",
                            "description": "Get the most recent anomalies (snapshot, not streaming)",
                            "inputSchema": {
                                "type": "object",
                                "properties": {
                                    "observer": { "type": "string", "description": "Filter by observer" },
                                    "max_events": { "type": "number", "default": 50, "description": "Maximum events to return" }
                                }
                            }
                        }
                    ]
                });
                mcp_write_result(id, tools)?;
            }
            "tools/call" => {
                let tool_name = params.get("name").and_then(|n| n.as_str()).unwrap_or("");
                let args = params
                    .get("arguments")
                    .cloned()
                    .unwrap_or(serde_json::json!({}));
                match mcp_call_tool(data_dir, tool_name, &args) {
                    Ok(result) => mcp_write_result(id, result)?,
                    Err(err) => mcp_write_error(
                        id,
                        -32603,
                        &format!("Tool call failed for '{tool_name}': {err}"),
                    )?,
                }
            }
            _ => {
                mcp_write_error(id, -32601, &format!("Method not found: {method}"))?;
            }
        }
    }

    Ok(())
}

fn mcp_call_tool(
    data_dir: &Path,
    name: &str,
    args: &serde_json::Value,
) -> anyhow::Result<serde_json::Value> {
    match name {
        "tapio_recent_anomalies" => {
            let minutes = args.get("minutes").and_then(|v| v.as_f64()).unwrap_or(5.0);
            let observer = args
                .get("observer")
                .and_then(|v| v.as_str())
                .unwrap_or("all");
            let severity = args
                .get("severity")
                .and_then(|v| v.as_str())
                .unwrap_or("all");

            let mut occs = load_occurrences(data_dir)?;
            let cutoff = (chrono::Utc::now() - chrono::Duration::seconds((minutes * 60.0) as i64))
                .to_rfc3339();
            occs.retain(|o| o.timestamp >= cutoff);

            if observer != "all" {
                occs.retain(|o| observer_category(&o.occurrence_type) == observer);
            }
            if severity != "all" {
                occs.retain(|o| {
                    let s = match o.severity {
                        Severity::Warning => "warning",
                        Severity::Error => "error",
                        Severity::Critical => "critical",
                        _ => "other",
                    };
                    s == severity
                });
            }

            occs.sort_by(|a, b| b.timestamp.cmp(&a.timestamp));
            let content = serde_json::to_string(&occs)?;
            Ok(serde_json::json!({
                "content": [{ "type": "text", "text": content }]
            }))
        }
        "tapio_node_health" => {
            let occs = load_occurrences(data_dir)?;
            let one_hour_ago = (chrono::Utc::now() - chrono::Duration::hours(1)).to_rfc3339();
            let recent: Vec<&Occurrence> = occs
                .iter()
                .filter(|o| o.timestamp >= one_hour_ago)
                .collect();

            let mut by_observer: HashMap<&str, (usize, usize)> = HashMap::new();
            for occ in &recent {
                let cat = observer_category(&occ.occurrence_type);
                let entry = by_observer.entry(cat).or_insert((0, 0));
                entry.0 += 1;
                if matches!(occ.severity, Severity::Critical) {
                    entry.1 += 1;
                }
            }

            let total: usize = by_observer.values().map(|(c, _)| c).sum();
            let total_critical: usize = by_observer.values().map(|(_, c)| c).sum();

            let health = serde_json::json!({
                "status": health_status(total, total_critical),
                "total_anomalies_1h": total,
                "total_critical_1h": total_critical,
                "observers": by_observer.iter().map(|(k, (count, critical))| {
                    serde_json::json!({ "name": k, "anomalies": count, "critical": critical })
                }).collect::<Vec<_>>(),
            });

            let content = serde_json::to_string_pretty(&health)?;
            Ok(serde_json::json!({
                "content": [{ "type": "text", "text": content }]
            }))
        }
        "tapio_watch_stream" => {
            let observer = args.get("observer").and_then(|v| v.as_str());
            let max = args
                .get("max_events")
                .and_then(|v| v.as_u64())
                .unwrap_or(50) as usize;

            let mut occs = load_occurrences(data_dir)?;
            if let Some(obs) = observer {
                occs.retain(|o| observer_category(&o.occurrence_type) == obs);
            }
            occs.sort_by(|a, b| b.timestamp.cmp(&a.timestamp));
            occs.truncate(max);

            let content = serde_json::to_string(&occs)?;
            Ok(serde_json::json!({
                "content": [{ "type": "text", "text": content }]
            }))
        }
        _ => Ok(serde_json::json!({
            "content": [{ "type": "text", "text": format!("Unknown tool: {name}") }],
            "isError": true,
        })),
    }
}

fn mcp_write_result(id: Option<serde_json::Value>, result: serde_json::Value) -> io::Result<()> {
    let response = serde_json::json!({
        "jsonrpc": "2.0",
        "id": id,
        "result": result,
    });
    let mut out = io::stdout().lock();
    serde_json::to_writer(&mut out, &response).map_err(io::Error::other)?;
    writeln!(out)?;
    out.flush()
}

fn mcp_write_error(id: Option<serde_json::Value>, code: i32, message: &str) -> io::Result<()> {
    let response = serde_json::json!({
        "jsonrpc": "2.0",
        "id": id,
        "error": { "code": code, "message": message },
    });
    let mut out = io::stdout().lock();
    serde_json::to_writer(&mut out, &response).map_err(io::Error::other)?;
    writeln!(out)?;
    out.flush()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn health_status(total_count: usize, total_critical: usize) -> &'static str {
    if total_critical >= 3 || total_count >= 20 {
        "critical"
    } else if total_count > 0 {
        "degraded"
    } else {
        "healthy"
    }
}

fn load_occurrences(data_dir: &Path) -> anyhow::Result<Vec<Occurrence>> {
    let mut occurrences = Vec::new();
    if !data_dir.exists() {
        return Ok(occurrences);
    }
    for entry in std::fs::read_dir(data_dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.extension().is_some_and(|e| e == "json")
            && let Ok(data) = std::fs::read_to_string(&path)
            && let Ok(occ) = serde_json::from_str::<Occurrence>(&data)
        {
            occurrences.push(occ);
        }
    }
    Ok(occurrences)
}

fn observer_category(occurrence_type: &str) -> &str {
    occurrence_type.split('.').nth(1).unwrap_or("unknown")
}

fn matches_filter(occ: &Occurrence, filters: &[&str]) -> bool {
    if filters.is_empty() {
        return true;
    }
    let cat = observer_category(&occ.occurrence_type);
    filters.contains(&cat)
}

fn write_occurrence(out: &mut impl Write, occ: &Occurrence, json: bool) -> io::Result<()> {
    if json {
        serde_json::to_writer(&mut *out, occ).map_err(io::Error::other)?;
        writeln!(out)?;
    } else {
        let severity = severity_label(&occ.severity);
        let ts = format_timestamp(&occ.timestamp);
        writeln!(out, "{ts} {severity:<8}  {}", occ.occurrence_type)?;
        if let Some(ref err) = occ.error {
            writeln!(out, "  {}", err.message)?;
        }
    }
    Ok(())
}

fn severity_label(s: &Severity) -> &'static str {
    match s {
        Severity::Debug => "DEBUG",
        Severity::Info => "INFO",
        Severity::Warning => "WARNING",
        Severity::Error => "ERROR",
        Severity::Critical => "CRITICAL",
    }
}

fn format_timestamp(ts: &str) -> String {
    if ts.len() >= 19 {
        ts[..19].to_string()
    } else {
        ts.to_string()
    }
}

fn parse_duration(s: &str) -> anyhow::Result<chrono::Duration> {
    let s = s.trim();
    if s.len() < 2 {
        anyhow::bail!("invalid duration: {s}");
    }
    let (num_str, unit) = s.split_at(s.len() - 1);
    let num: i64 = num_str
        .parse()
        .map_err(|_| anyhow::anyhow!("invalid duration number: {num_str}"))?;
    match unit {
        "s" => Ok(chrono::Duration::seconds(num)),
        "m" => Ok(chrono::Duration::minutes(num)),
        "h" => Ok(chrono::Duration::hours(num)),
        "d" => Ok(chrono::Duration::days(num)),
        _ => anyhow::bail!("unknown duration unit: {unit} (use s/m/h/d)"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_duration_minutes() {
        let d = parse_duration("5m").unwrap();
        assert_eq!(d, chrono::Duration::minutes(5));
    }

    #[test]
    fn parse_duration_hours() {
        let d = parse_duration("1h").unwrap();
        assert_eq!(d, chrono::Duration::hours(1));
    }

    #[test]
    fn parse_duration_seconds() {
        let d = parse_duration("30s").unwrap();
        assert_eq!(d, chrono::Duration::seconds(30));
    }

    #[test]
    fn parse_duration_invalid() {
        assert!(parse_duration("abc").is_err());
        assert!(parse_duration("5x").is_err());
        assert!(parse_duration("").is_err());
    }

    #[test]
    fn observer_category_network() {
        assert_eq!(
            observer_category("kernel.network.retransmit_spike"),
            "network"
        );
    }

    #[test]
    fn observer_category_container() {
        assert_eq!(observer_category("kernel.container.oom_kill"), "container");
    }

    #[test]
    fn observer_category_malformed() {
        assert_eq!(observer_category("notseparated"), "unknown");
    }

    #[test]
    fn filter_matches_all_when_empty() {
        let occ = Occurrence::new(
            "kernel.network.test",
            Severity::Info,
            tapio_common::occurrence::Outcome::Success,
        );
        assert!(matches_filter(&occ, &[]));
    }

    #[test]
    fn filter_matches_selected() {
        let occ = Occurrence::new(
            "kernel.network.test",
            Severity::Info,
            tapio_common::occurrence::Outcome::Success,
        );
        assert!(matches_filter(&occ, &["network"]));
        assert!(!matches_filter(&occ, &["container"]));
    }

    #[test]
    fn format_timestamp_trims_to_datetime() {
        let ts = "2026-03-29T14:23:01.123456789+00:00";
        assert_eq!(format_timestamp(ts), "2026-03-29T14:23:01");
    }

    #[test]
    fn health_status_healthy() {
        assert_eq!(health_status(0, 0), "healthy");
    }

    #[test]
    fn health_status_degraded() {
        assert_eq!(health_status(1, 0), "degraded");
        assert_eq!(health_status(19, 2), "degraded");
    }

    #[test]
    fn health_status_critical_by_count() {
        assert_eq!(health_status(20, 0), "critical");
        assert_eq!(health_status(100, 0), "critical");
    }

    #[test]
    fn health_status_critical_by_severity() {
        assert_eq!(health_status(3, 3), "critical");
        assert_eq!(health_status(5, 10), "critical");
    }
}
