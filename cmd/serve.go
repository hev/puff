package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/hev/tpuff/internal/client"
	"github.com/hev/tpuff/internal/config"
	"github.com/hev/tpuff/internal/searchapp"
	"github.com/spf13/cobra"
)

func init() {
	command := &cobra.Command{Use: "serve", Short: "Open a local schema-driven search app", Args: cobra.NoArgs, RunE: runServe}
	command.Flags().StringP("namespace", "n", "", "Namespace to serve (read-only)")
	command.Flags().StringP("region", "r", "", "Override the region")
	command.Flags().String("fts", "", "Full-text query field")
	command.Flags().String("app", "", "Versioned search-app.json definition")
	command.Flags().String("ui-dir", "", "Verified shared UI runtime bundle (development)")
	command.Flags().Int("port", 0, "Loopback port (0 selects an available port)")
	command.Flags().Bool("no-open", false, "Print the URL without opening a browser")
	_ = command.MarkFlagRequired("namespace")
	rootCmd.AddCommand(command)
}

func runServe(cmd *cobra.Command, _ []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	region, _ := cmd.Flags().GetString("region")
	fts, _ := cmd.Flags().GetString("fts")
	appPath, _ := cmd.Flags().GetString("app")
	directory, _ := cmd.Flags().GetString("ui-dir")
	port, _ := cmd.Flags().GetInt("port")
	noOpen, _ := cmd.Flags().GetBool("no-open")
	if port < 0 || port > 65535 {
		return errors.New("port must be between 0 and 65535")
	}
	definition, err := searchapp.LoadDefinition(appPath)
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("fts") {
		definition.QueryField = fts
	}
	assets, err := searchapp.Assets(directory)
	if err != nil {
		return err
	}
	ns, err := client.GetNamespace(namespace, region)
	if err != nil {
		return err
	}
	host, err := searchapp.New(searchapp.SDKBackend{Namespace: ns}, assets, namespace, definition, config.GetActiveContentField(namespace))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = host.Check(checkCtx)
	cancel()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", port, err)
	}
	url := host.Bind(listener.Addr().String())
	server := &http.Server{Handler: host, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), url)
	if !noOpen {
		if err := openSearchBrowser(url); err != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Browser could not open; use the URL above.")
		}
	}
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}

func openSearchBrowser(url string) error {
	var command string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
	case "linux":
		command = "xdg-open"
	default:
		return errors.New("browser launch unsupported")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, command, url).Run()
}
