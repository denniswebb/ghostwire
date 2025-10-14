package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/denniswebb/ghostwire/internal/logging"
)

var (
	cfgFile string
)

var rootCmd = &cobra.Command{
	Use:   "ghostwire",
	Short: "Invisible in-cluster traffic switcher for Blue/Green & Canary rollouts",
	Long: `ghostwire makes pods labeled as "preview" route to matching preview services (like "*-preview") instead of the active ones.
It does this at L4 with DNAT rules. No app code changes, no mesh dependency, no DNS roulette. You choose the labels, patterns, and behavior.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		viper.SetEnvPrefix("GW")
		viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
		viper.AutomaticEnv()

		if cfgFile != "" {
			viper.SetConfigFile(cfgFile)
			if err := viper.ReadInConfig(); err != nil {
				return fmt.Errorf("failed to read config file: %w", err)
			}
		}

		logging.InitLogger(viper.GetString("log-level"), "ghostwire")
		return nil
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "Path to configuration file")
	rootCmd.PersistentFlags().String("log-level", "info", "Log level (debug, info, warn, error)")

	if err := viper.BindPFlag("log-level", rootCmd.PersistentFlags().Lookup("log-level")); err != nil {
		fmt.Fprintf(os.Stderr, "failed to bind log-level flag: %v\n", err)
		os.Exit(1)
	}

	viper.SetDefault("namespace", "default")
	viper.SetDefault("svc-preview-pattern", "{{name}}-preview")
	viper.SetDefault("active-suffix", "-active")
	viper.SetDefault("preview-suffix", "-preview")

	rootCmd.AddCommand(InitCmd)
	rootCmd.AddCommand(WatcherCmd)
	rootCmd.AddCommand(InjectorCmd)
}
