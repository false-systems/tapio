mod commands;
mod store;

use clap::Parser;
use std::path::PathBuf;

#[derive(Parser)]
#[command(name = "tapio", version, about = "Kernel eyes for Kubernetes")]
struct Cli {
    /// Path to occurrence data directory
    #[arg(long, default_value = ".tapio/occurrences")]
    data_dir: PathBuf,

    /// Output as JSON instead of human-readable format
    #[arg(long)]
    json: bool,

    #[command(subcommand)]
    command: Option<Command>,
}

#[derive(clap::Subcommand)]
enum Command {
    /// Show observer status and event summary
    Status,
    /// Recent anomalies on this node
    Recent {
        /// Maximum number of anomalies to show
        #[arg(long, default_value = "20")]
        limit: usize,
    },
    /// Show full occurrence details by ID
    Show {
        /// Occurrence ID (ULID)
        id: String,
    },
    /// Live stream of kernel anomalies
    Watch,
    /// Node health: severity and type aggregates
    Health,
    /// All kernel events for a pod
    Explain {
        /// Pod name or partial match
        pod: String,
    },
    /// Start MCP server (stdio transport)
    Mcp,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();
    let dir = &cli.data_dir;
    let json = cli.json;

    match cli.command {
        None | Some(Command::Status) => commands::status::run(dir, json),
        Some(Command::Recent { limit }) => commands::recent::run(dir, limit, json),
        Some(Command::Show { id }) => commands::show::run(dir, &id, json),
        Some(Command::Watch) => commands::watch::run(dir, json).await,
        Some(Command::Health) => commands::health::run(dir, json),
        Some(Command::Explain { pod }) => commands::explain::run(dir, &pod, json),
        Some(Command::Mcp) => {
            println!("MCP server not yet implemented");
            Ok(())
        }
    }
}
