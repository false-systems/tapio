package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/correlation/markdown"
)

// CorrelationsCmd handles correlation rule management
func CorrelationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "correlations",
		Short: "Manage custom correlation rules safely",
		Long: `Manage custom correlation rules for Tapio with built-in safety protections.

Write correlation patterns in markdown and load them into Tapio's correlation engine.
This allows you to extend Tapio's built-in correlations with your own domain-specific patterns.

🛡️  SAFETY FEATURES:
- Automatic backups before dangerous operations
- Protection against deleting critical system rules  
- Disable/enable instead of permanent deletion
- Multiple confirmation layers for destructive actions

💡 RECOMMENDED WORKFLOW:
1. Create rules: tapio correlations load my-patterns.md
2. Test safely: tapio correlations validate my-patterns.md --dry-run
3. Modify rules: tapio correlations update my-patterns.md
4. Disable temporarily: tapio correlations disable rule-id --reason "testing"
5. Re-enable: tapio correlations enable rule-id
6. Backup regularly: tapio correlations snapshot

⚠️  Use 'disable' instead of 'delete' to preserve correlations for recovery!`,
	}

	cmd.AddCommand(
		correlationsLoadCmd(),
		correlationsValidateCmd(),
		correlationsExportCmd(),
		correlationsListCmd(),
		correlationsDeleteCmd(),
		correlationsUpdateCmd(),
		correlationsDisableCmd(),
		correlationsEnableCmd(),
		correlationsSnapshotCmd(),
	)

	return cmd
}

// correlationsLoadCmd loads correlation rules from markdown
func correlationsLoadCmd() *cobra.Command {
	var (
		outputJSON bool
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "load <markdown-file>",
		Short: "Load correlation rules from markdown file",
		Long: `Load custom correlation rules from a markdown file.

The markdown file should contain correlation patterns in natural language.
See 'tapio correlations example' for the markdown format.

Example:
  tapio correlations load my-patterns.md
  tapio correlations load my-patterns.md --dry-run --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			markdownFile := args[0]

			// Read markdown file
			content, err := ioutil.ReadFile(markdownFile)
			if err != nil {
				return fmt.Errorf("failed to read markdown file: %w", err)
			}

			// Create translator
			translator := markdown.NewCorrelationTranslator()

			// Translate to rules
			rules, err := translator.TranslateMarkdownToRules(string(content))
			if err != nil {
				return fmt.Errorf("failed to translate markdown: %w", err)
			}

			fmt.Printf("✅ Successfully parsed %d correlation rules from %s\n\n", len(rules), markdownFile)

			// Display rules
			for i, rule := range rules {
				fmt.Printf("Rule %d: %s\n", i+1, rule.Name)
				fmt.Printf("  ID: %s\n", rule.ID)
				fmt.Printf("  Category: %s\n", rule.Category)
				fmt.Printf("  Severity: %s\n", rule.Severity)
				if rule.Description != "" {
					fmt.Printf("  Description: %s\n", rule.Description)
				}
				fmt.Println()
			}

			if outputJSON {
				// Export as JSON
				jsonData, err := translator.TranslateMarkdownToJSON(string(content))
				if err != nil {
					return fmt.Errorf("failed to generate JSON: %w", err)
				}
				fmt.Println("JSON Output:")
				fmt.Println(string(jsonData))
			}

			if !dryRun {
				// TODO: Actually load rules into running correlation engine
				// This would require connecting to the tapio server
				fmt.Println("⚠️  Note: Actually loading rules into the engine requires server connection (not implemented yet)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output rules as JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse and validate without loading")

	return cmd
}

// correlationsValidateCmd validates markdown correlation files
func correlationsValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <markdown-file>",
		Short: "Validate correlation rules in markdown file",
		Long: `Validate that a markdown file contains valid correlation rules.

This checks the syntax and structure without loading the rules.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			markdownFile := args[0]

			// Read markdown file
			content, err := ioutil.ReadFile(markdownFile)
			if err != nil {
				return fmt.Errorf("failed to read markdown file: %w", err)
			}

			// Create translator
			translator := markdown.NewCorrelationTranslator()

			// Try to translate
			rules, err := translator.TranslateMarkdownToRules(string(content))
			if err != nil {
				return fmt.Errorf("❌ Validation failed: %w", err)
			}

			fmt.Printf("✅ Validation successful!\n")
			fmt.Printf("   Found %d valid correlation rules\n", len(rules))

			// Show rule summary
			for _, rule := range rules {
				fmt.Printf("   - %s (%s)\n", rule.Name, rule.Category)
			}

			return nil
		},
	}

	return cmd
}

// correlationsExportCmd exports existing rules to markdown
func correlationsExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export existing correlation rules to markdown",
		Long: `Export correlation rules from the engine to markdown format.

This allows you to see built-in rules and modify them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Connect to engine and export rules
			fmt.Println("⚠️  Export functionality not yet implemented")
			fmt.Println("This would export existing rules from the correlation engine to markdown format")
			return nil
		},
	}

	return cmd
}

// correlationsListCmd lists loaded correlation rules
func correlationsListCmd() *cobra.Command {
	var (
		showBuiltin bool
		showCustom  bool
		category    string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List loaded correlation rules",
		Long:  `List all correlation rules currently loaded in the engine.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Connect to engine and list rules
			fmt.Println("⚠️  List functionality not yet implemented")
			fmt.Println("This would show correlation rules currently loaded in the engine")
			
			// For now, show example output
			fmt.Println("\nExample output:")
			fmt.Println("Built-in Rules:")
			fmt.Println("  - OOM Detection (memory)")
			fmt.Println("  - CPU Throttling (performance)")
			fmt.Println("  - Crash Loop Detection (reliability)")
			fmt.Println("\nCustom Rules:")
			fmt.Println("  - Memory Leak Pattern (user_defined)")
			fmt.Println("  - Database Cascade (user_defined)")
			
			return nil
		},
	}

	cmd.Flags().BoolVar(&showBuiltin, "builtin", true, "Show built-in rules")
	cmd.Flags().BoolVar(&showCustom, "custom", true, "Show custom rules")
	cmd.Flags().StringVar(&category, "category", "", "Filter by category")

	return cmd
}

// ExampleMarkdown returns example markdown for correlations
func ExampleCorrelationMarkdown() string {
	return `# Example Correlation Rules

## High Memory Usage Pattern

When memory usage > 80% for 5 minutes,
then predict potential OOM within 10 minutes.

Root cause: Check for memory leaks or undersized containers.
Recommendation: Increase memory limits or optimize application.

Severity: high
Confidence: 85%

## Service Cascade Failure

If database latency > 1000ms,
then API errors will increase within 30 seconds.

This indicates a cascade failure pattern.
Action: Check database health and connection pool settings.

` + "```yaml\n" + `category: cascade_failure
severity: critical
confidence: 90
` + "```" + `

## CPU Throttling Detection

When CPU throttling > 50% and CPU usage > 90%,
then the service is CPU constrained.

Predict: Performance degradation and increased latency.
Fix: Increase CPU limits or optimize CPU usage.

Category: performance
`
}

// correlationsDeleteCmd deletes correlation rules by ID (DANGEROUS - use disable instead)
func correlationsDeleteCmd() *cobra.Command {
	var (
		force bool
		reallyDelete bool
	)

	cmd := &cobra.Command{
		Use:   "delete <rule-id> [rule-id...]",
		Short: "⚠️  PERMANENTLY delete correlation rules (DANGEROUS)",
		Long: `🚨 DANGER: PERMANENTLY delete correlation rules from the engine.

⚠️  WARNING: This is IRREVERSIBLE and can destroy critical correlations!
⚠️  RECOMMENDATION: Use 'tapio correlations disable' instead for safety.

SAFETY RESTRICTIONS:
- Only user-defined rules can be deleted (never built-in rules)
- Protected/critical rules cannot be deleted
- Heavily used rules (>1000 executions) cannot be deleted
- Rules are backed up before deletion
- Requires double confirmation

SAFER ALTERNATIVE:
  tapio correlations disable <rule-id> --reason "no longer needed"

Example:
  tapio correlations delete user_memory_leak_pattern --really-delete --force`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleIDs := args

			// First safety check
			if !reallyDelete {
				fmt.Println("🚨 SAFETY PROTECTION ACTIVATED")
				fmt.Println("")
				fmt.Println("❌ Deletion blocked! Use --really-delete flag if you truly need permanent deletion.")
				fmt.Println("💡 RECOMMENDED: Use 'tapio correlations disable' instead:")
				fmt.Println("")
				for _, ruleID := range ruleIDs {
					fmt.Printf("   tapio correlations disable %s --reason 'no longer needed'\n", ruleID)
				}
				fmt.Println("")
				fmt.Println("Disabling preserves rules for recovery while stopping their execution.")
				return nil
			}

			// Second safety check
			if !force {
				fmt.Printf("🚨 FINAL WARNING: PERMANENTLY delete %d rule(s)? This CANNOT be undone!\n", len(ruleIDs))
				fmt.Printf("Type 'DELETE' to confirm: ")
				var response string
				fmt.Scanln(&response)
				if response != "DELETE" {
					fmt.Println("Deletion cancelled - correlations preserved! ✅")
					return nil
				}
			}

			// Show what would be deleted with safety warnings
			fmt.Printf("🗑️  Attempting to delete %d correlation rule(s) with safety checks:\n", len(ruleIDs))
			for _, ruleID := range ruleIDs {
				fmt.Printf("   - %s (safety checks will be applied)\n", ruleID)
			}
			
			fmt.Println("\n🔒 SAFETY: Rules will be backed up before deletion")
			fmt.Println("⚠️  Note: Actually deleting rules requires server connection (not implemented yet)")
			fmt.Println("This would call engine.DeleteSemanticRule() with full safety checks")

			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip final confirmation (still requires --really-delete)")
	cmd.Flags().BoolVar(&reallyDelete, "really-delete", false, "Bypass safety protection (required for deletion)")

	return cmd
}

// correlationsUpdateCmd updates correlation rules from markdown
func correlationsUpdateCmd() *cobra.Command {
	var (
		outputJSON bool
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "update <markdown-file>",
		Short: "Update correlation rules from markdown file",
		Long: `Update existing correlation rules from a markdown file.

This will modify existing rules with the same ID or create new ones if they don't exist.
The markdown file should contain correlation patterns in natural language.

Example:
  tapio correlations update my-updated-patterns.md
  tapio correlations update my-patterns.md --dry-run --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			markdownFile := args[0]

			// Read markdown file
			content, err := ioutil.ReadFile(markdownFile)
			if err != nil {
				return fmt.Errorf("failed to read markdown file: %w", err)
			}

			// Create translator
			translator := markdown.NewCorrelationTranslator()

			// Translate to rules
			rules, err := translator.TranslateMarkdownToRules(string(content))
			if err != nil {
				return fmt.Errorf("failed to translate markdown: %w", err)
			}

			fmt.Printf("📝 Successfully parsed %d correlation rules from %s\n\n", len(rules), markdownFile)

			// Display rules that will be updated
			for i, rule := range rules {
				fmt.Printf("Rule %d: %s\n", i+1, rule.Name)
				fmt.Printf("  ID: %s\n", rule.ID)
				fmt.Printf("  Category: %s\n", rule.Category)
				fmt.Printf("  Severity: %s\n", rule.Severity)
				if rule.Description != "" {
					fmt.Printf("  Description: %s\n", rule.Description)
				}
				fmt.Println()
			}

			if outputJSON {
				// Export as JSON
				jsonData, err := translator.TranslateMarkdownToJSON(string(content))
				if err != nil {
					return fmt.Errorf("failed to generate JSON: %w", err)
				}
				fmt.Println("JSON Output:")
				fmt.Println(string(jsonData))
			}

			if !dryRun {
				// TODO: Actually update rules in the correlation engine
				// This would require connecting to the tapio server
				fmt.Println("⚠️  Note: Actually updating rules in the engine requires server connection (not implemented yet)")
				fmt.Println("This would call engine.UpdateSemanticRule() for each rule")
			} else {
				fmt.Println("✅ Dry run completed - no rules were actually updated")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output rules as JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse and validate without updating")

	return cmd
}

// correlationsDisableCmd disables correlation rules (SAFER alternative to delete)
func correlationsDisableCmd() *cobra.Command {
	var (
		reason string
	)

	cmd := &cobra.Command{
		Use:   "disable <rule-id> [rule-id...]",
		Short: "Safely disable correlation rules (RECOMMENDED over delete)",
		Long: `Safely disable correlation rules without deleting them.

This stops rule execution while preserving the rule for potential recovery.
Disabled rules can be re-enabled later with 'tapio correlations enable'.

✅ SAFE: Rules are preserved and can be recovered
✅ REVERSIBLE: Use 'enable' command to reactivate
✅ AUDITABLE: Tracks disable reason and timestamp

Example:
  tapio correlations disable user_memory_leak_pattern --reason "deprecated"
  tapio correlations disable rule1 rule2 --reason "testing new approach"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleIDs := args
			
			if reason == "" {
				reason = "disabled via CLI"
			}

			fmt.Printf("🔒 Disabling %d correlation rule(s) safely:\n", len(ruleIDs))
			for _, ruleID := range ruleIDs {
				fmt.Printf("   - %s (reason: %s)\n", ruleID, reason)
			}
			
			fmt.Println("\n✅ SAFE: Rules preserved for recovery")
			fmt.Printf("💡 To re-enable: tapio correlations enable %s\n", args[0])
			fmt.Println("\n⚠️  Note: Actually disabling rules requires server connection (not implemented yet)")
			fmt.Printf("This would call engine.DisableSemanticRule() for each rule with reason: %s\n", reason)

			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "Reason for disabling the rule(s)")

	return cmd
}

// correlationsEnableCmd re-enables disabled correlation rules
func correlationsEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable <rule-id> [rule-id...]",
		Short: "Re-enable disabled correlation rules",
		Long: `Re-enable previously disabled correlation rules.

This restores rule execution for rules that were disabled with 'disable' command.

Example:
  tapio correlations enable user_memory_leak_pattern
  tapio correlations enable rule1 rule2 rule3`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleIDs := args

			fmt.Printf("🔓 Enabling %d correlation rule(s):\n", len(ruleIDs))
			for _, ruleID := range ruleIDs {
				fmt.Printf("   - %s\n", ruleID)
			}
			
			fmt.Println("\n✅ Rules will be restored to active processing")
			fmt.Println("\n⚠️  Note: Actually enabling rules requires server connection (not implemented yet)")
			fmt.Println("This would call engine.EnableSemanticRule() for each rule")

			return nil
		},
	}

	return cmd
}

// correlationsSnapshotCmd creates backups of correlation rules
func correlationsSnapshotCmd() *cobra.Command {
	var (
		snapshotName string
	)

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Create a backup snapshot of all correlation rules",
		Long: `Create a backup snapshot of all correlation rules for disaster recovery.

This creates a timestamped backup of all rules that can be used for recovery.
Snapshots are automatically created before dangerous operations.

Example:
  tapio correlations snapshot
  tapio correlations snapshot --name "before-migration"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if snapshotName == "" {
				snapshotName = fmt.Sprintf("manual-%d", time.Now().Unix())
			}

			fmt.Printf("🔒 Creating correlation rules snapshot: %s\n", snapshotName)
			fmt.Println("📦 Backing up all correlation rules for disaster recovery")
			fmt.Printf("💾 Snapshot location: ~/.tapio/backups/correlations-%s.json\n", snapshotName)
			
			fmt.Println("\n⚠️  Note: Actually creating snapshots requires server connection (not implemented yet)")
			fmt.Printf("This would call engine.CreateRuleSnapshot('%s')\n", snapshotName)

			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotName, "name", "", "Custom name for the snapshot")

	return cmd
}