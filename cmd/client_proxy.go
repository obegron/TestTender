package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/obegron/testtender/internal/clientproxy"
)

var clientProxyConfig struct {
	listenAddress string
	upstream      string
	tokenFile     string
	caFile        string
}

var clientProxyCmd = &cobra.Command{
	Use:   "client-proxy",
	Short: "Inject a projected OIDC token into Docker API requests",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := validateLoopbackListenAddress(clientProxyConfig.listenAddress); err != nil {
			return err
		}
		proxy, err := clientproxy.New(clientproxy.Config{
			Upstream:  clientProxyConfig.upstream,
			TokenFile: clientProxyConfig.tokenFile,
			CAFile:    clientProxyConfig.caFile,
		})
		if err != nil {
			return err
		}

		server := &http.Server{
			Addr:              clientProxyConfig.listenAddress,
			Handler:           proxy,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       90 * time.Second,
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		errCh := make(chan error, 1)
		go func() {
			errCh <- server.ListenAndServe()
		}()

		select {
		case err := <-errCh:
			if !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("shutdown client proxy: %w", err)
			}
			return nil
		}
	},
}

func validateLoopbackListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse client proxy listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("client proxy must listen on a literal loopback address")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(clientProxyCmd)
	clientProxyCmd.Flags().StringVar(&clientProxyConfig.listenAddress, "listen-addr", "127.0.0.1:2475", "Local Docker API listen address")
	clientProxyCmd.Flags().StringVar(&clientProxyConfig.upstream, "upstream", "", "HTTPS TestTender API URL")
	clientProxyCmd.Flags().StringVar(&clientProxyConfig.tokenFile, "token-file", "", "Absolute path to the projected OIDC token")
	clientProxyCmd.Flags().StringVar(&clientProxyConfig.caFile, "ca-file", "", "Additional PEM CA bundle for the TestTender endpoint")
	_ = clientProxyCmd.MarkFlagRequired("upstream")
	_ = clientProxyCmd.MarkFlagRequired("token-file")
}
