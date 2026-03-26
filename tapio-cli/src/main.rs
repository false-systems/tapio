use clap::Parser;

#[derive(Parser)]
#[command(name = "tapio", version, about = "Kernel eyes for Kubernetes")]
enum Cli {
    /// Show observer status and event rates
    Status,
    /// Live stream of kernel anomalies
    Watch,
    /// Recent anomalies on this node
    Recent,
    /// Node health: network, storage, memory, cpu
    Health,
    /// Start MCP server (stdio transport)
    Mcp,
}

fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();

    match cli {
        Cli::Status => println!("tapio v4 — not yet observing"),
        Cli::Watch => println!("watching..."),
        Cli::Recent => println!("no anomalies yet"),
        Cli::Health => println!("healthy (no observers attached)"),
        Cli::Mcp => println!("MCP server not yet implemented"),
    }

    Ok(())
}
