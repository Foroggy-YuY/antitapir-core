package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/cmd/sing-box/internal/control"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	"github.com/spf13/cobra"
)

var managedTimeout time.Duration
var memoryConfig bool

func init() {
	command := &cobra.Command{
		Use: "managed", Short: "Run Anti-Tapir Core over inherited stdio", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if managedTimeout <= 0 || managedTimeout > time.Hour {
				return errors.New("invalid_timeout")
			}
			for _, path := range configPaths {
				if path == "stdin" {
					return errors.New("config_stdin_reserved_for_control")
				}
			}
			// Preserve stdout for protocol messages even if an imported config asks
			// for console logs. Never open a user-specified log output file here.
			return control.Serve(os.Stdin, os.Stdout, func(ctx context.Context, event func(string) error, content []byte) (func() error, error) {
				var options option.Options
				var err error
				if memoryConfig {
					if len(content) == 0 || len(content) > 1024*1024 {
						return nil, errors.New("invalid_config")
					}
					options, err = sjson.UnmarshalExtendedContext[option.Options](globalCtx, content)
				} else {
					options, err = readConfigAndMerge()
				}
				if err != nil {
					return nil, err
				}
				// Lifecycle diagnostics are structured and secret-free in v1.
				// Imported logs could contain credentials in tags or URLs.
				options.Log = &option.LogOptions{Disabled: true}
				if err = event("config_accepted"); err != nil {
					return nil, err
				}
				instance, err := box.New(box.Options{Context: withManagedContext(ctx), Options: options})
				if err != nil {
					return nil, err
				}
				return instance.Close, instance.Start()
			}, managedTimeout)
		},
	}
	command.Flags().DurationVar(&managedTimeout, "control-timeout", 60*time.Second, "startup/shutdown deadline")
	command.Flags().BoolVar(&memoryConfig, "config-stdin", false, "receive configuration through private control pipe")
	mainCommand.AddCommand(command)
	mainCommand.AddCommand(&cobra.Command{
		Use: "core-info", Short: "Print Anti-Tapir Core identity", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"name": "antitapir-core", "version": control.Version, "protocol": control.Protocol,
				"upstream": "1.14.0-alpha.18", "maintainer": "Foroggy", "capabilities": []string{"memory-config"},
			})
		},
	})
}

// Keep the upstream service registry while allowing parent cancellation.
type managedContext struct {
	context.Context
	parent context.Context
}

func (c managedContext) Value(key any) any                   { return c.parent.Value(key) }
func withManagedContext(ctx context.Context) context.Context { return managedContext{ctx, globalCtx} }
