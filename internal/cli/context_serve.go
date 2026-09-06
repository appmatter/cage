package cli

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/appmatter/cage/internal/contextapi"
)

func newContextServeCmd() *cobra.Command {
	var (
		projectRoot  string
		configs      []string
		token        string
		addr         string
		allowedHosts []string
	)
	cmd := &cobra.Command{
		Use:    "context-serve",
		Hidden: true,
		Short:  "Serve the host-only per-project client context API",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextServe(cmd.OutOrStdout(), projectRoot, configs, token, addr, allowedHosts)
		},
	}
	cmd.Flags().StringVar(&projectRoot, "project", ".", "project root")
	cmd.Flags().StringArrayVar(&configs, "config", nil, "allowlisted config path under the project (repeatable; default: resolved active config)")
	cmd.Flags().StringVar(&token, "token", "", "bearer token (default: generated)")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:0", "loopback listen address")
	cmd.Flags().StringArrayVar(&allowedHosts, "allowed-host", nil, "allowed Host header (repeatable; default: no Host check)")
	return cmd
}

func runContextServe(w io.Writer, projectRoot string, configs []string, token, addr string, allowedHosts []string) error {
	srv, ln, err := startContextServe(w, projectRoot, configs, token, addr, allowedHosts)
	if err != nil {
		return err
	}
	defer srv.Close()
	defer ln.Close()
	return srv.Serve(ln)
}

func startContextServe(w io.Writer, projectRoot string, configs []string, token, addr string, allowedHosts []string) (*contextapi.Server, net.Listener, error) {
	if token == "" {
		generated, err := contextapi.NewToken()
		if err != nil {
			return nil, nil, err
		}
		token = generated
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, nil, err
	}
	project, err := contextapi.LoadProject(root, configs, runtime.GOOS)
	if err != nil {
		return nil, nil, err
	}
	srv := contextapi.New(contextapi.Options{
		Project:      project,
		Authorize:    contextapi.BearerTokens(map[string]contextapi.TokenGrant{token: {}}),
		AllowedHosts: allowedHosts,
	})
	ln, err := srv.Listen(addr)
	if err != nil {
		srv.Close()
		return nil, nil, err
	}
	bound := ln.Addr().String()
	if err := contextapi.WriteServeState(root, contextapi.ServeState{
		Addr:         bound,
		Token:        token,
		PID:          os.Getpid(),
		AllowedHosts: allowedHosts,
	}); err != nil {
		ln.Close()
		srv.Close()
		return nil, nil, err
	}
	fmt.Fprintf(w, "http://%s/?token=%s\n", bound, url.QueryEscape(token))
	return srv, ln, nil
}
