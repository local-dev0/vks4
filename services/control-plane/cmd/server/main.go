package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vks4/vks4/services/control-plane/internal/app"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			migrateCmd(os.Args[2:])
			return
		case "health":
			if err := healthProbe(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "serve":
			// fallthrough
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
			os.Exit(2)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	a, err := app.Build(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	if err := a.Run(ctx); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "run:", err)
		os.Exit(1)
	}
}

func migrateCmd(args []string) {
	cmd := "up"
	if len(args) > 0 {
		cmd = args[0]
	}
	dbURL := os.Getenv("DATABASE_URL")
	path := os.Getenv("MIGRATIONS_PATH")
	switch cmd {
	case "up":
		if err := app.MigrateUp(dbURL, path); err != nil {
			fmt.Fprintln(os.Stderr, "migrate up:", err)
			os.Exit(1)
		}
	case "down":
		if err := app.MigrateDown(dbURL, path); err != nil {
			fmt.Fprintln(os.Stderr, "migrate down:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "migrate: unknown subcommand %q\n", cmd)
		os.Exit(2)
	}
}

func healthProbe() error {
	addr := os.Getenv("CONTROL_PLANE_HTTP")
	if addr == "" {
		addr = ":8080"
	}
	// Простая HTTP-проверка для Docker healthcheck.
	url := "http://localhost" + addr + "/health"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return nil
}
