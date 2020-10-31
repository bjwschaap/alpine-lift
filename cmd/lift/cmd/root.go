package cmd

import (
	"fmt"
	"os"

	"github.com/bjwschaap/alpine-lift/pkg/lift"
	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// RootCmd represents the base command when called without any subcommands
	RootCmd = &cobra.Command{
		Use:     "lift",
		Version: fmt.Sprintf("%s-%s (built by %s on %s)", version, gitTag, buildUser, buildDate),
		Short:   "A cloud-init alternative for Alpine Linux",
		Long:    `Lift performs initial OS configuration on first boot.`,
		Run: func(cmd *cobra.Command, args []string) {

			if viper.GetBool("debug") {
				log.SetLevel(log.DebugLevel)
			}

			if viper.GetBool("no-color") {
				logFormat = log.TextFormatter{
					ForceColors:     false,
					DisableColors:   true,
					FullTimestamp:   true,
					TimestampFormat: "2006-01-02T15:04:05.999999999",
				}
			}

			if viper.GetBool("json") {
				log.SetFormatter(&log.JSONFormatter{})
			}

			lift, err := lift.New(viper.GetString("alpine-data-url"))
			if err != nil {
				log.Error(err)
				log.Error("Lift aborted")
				os.Exit(1)
			}

			if err = lift.Start(); err != nil {
				log.Error(err)
				log.Error("Lift aborted")
				os.Exit(1)
			}
		},
	}

	logFormat = log.TextFormatter{
		ForceColors:     true,
		DisableColors:   false,
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05.999999999",
	}

	cfgFile string
	dataURL string
	debug   bool
	json    bool
	nocolor bool
)

func init() {
	// Default logging settings
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logFormat)
	log.SetLevel(log.InfoLevel)

	// Flags & Config
	cobra.OnInitialize(initConfig)
	RootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (e.g. $HOME/.lift)")
	RootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "enable debug logging")
	RootCmd.PersistentFlags().BoolVar(&nocolor, "no-color", false, "disable colors in logging")
	RootCmd.PersistentFlags().BoolVarP(&json, "json", "j", false, "Log output in JSON format")
	RootCmd.PersistentFlags().StringVarP(&dataURL, "alpine-data-url", "s", "", "URL to download alpine-data")
	_ = viper.BindPFlag("debug", RootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("alpine-data-url", RootCmd.PersistentFlags().Lookup("alpine-data-url"))
	_ = viper.BindPFlag("json", RootCmd.PersistentFlags().Lookup("json"))
	_ = viper.BindPFlag("no-color", RootCmd.PersistentFlags().Lookup("no-color"))
}

func initConfig() {
	// Automatically bind flags to LIFT_<flagname> environment variables
	viper.SetEnvPrefix("lift")
	viper.AutomaticEnv()

	// Try to load configuration file when specified
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Search config
		viper.AddConfigPath("/etc/lift/")
		viper.AddConfigPath(fmt.Sprintf("%s/.lift/", home))
		viper.AddConfigPath(".")
		viper.SetConfigName("config")
	}

	if err := viper.ReadInConfig(); err != nil {
		// Ignore, just use flags/env
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		log.Error(err)
		log.Error("Lift aborted")
		os.Exit(-1)
	}
}
