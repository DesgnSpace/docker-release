package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/malico/docker-release/internal/controller"
	"github.com/malico/docker-release/internal/docker"
	"github.com/malico/docker-release/internal/state"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "watch":
		run(cmdWatch)
	case "rollback":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: docker-release rollback <service>")
			os.Exit(1)
		}
		run(func(ctrl *controller.Controller) error {
			return ctrl.Rollback(context.Background(), os.Args[2])
		})
	case "status":
		service := ""
		if len(os.Args) >= 3 {
			service = os.Args[2]
		}
		run(func(ctrl *controller.Controller) error {
			return ctrl.Status(context.Background(), service)
		})
	case "version":
		fmt.Printf("docker-release %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func run(fn func(*controller.Controller) error) {
	dockerClient, err := docker.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer dockerClient.Close()

	stateDir := envOr("RELEASE_STATE_DIR", "/var/lib/docker-release")
	configDir := envOr("RELEASE_CONFIG_DIR", "/etc/docker-release/conf.d")
	mgr := state.NewManager(stateDir)
	ctrl := controller.New(dockerClient, mgr, configDir)

	if err := fn(ctrl); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdWatch(ctrl *controller.Controller) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return ctrl.Watch(ctx)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printUsage() {
	fmt.Printf(`docker-release %s
Deployment controller for Docker Compose environments.

Usage:
  docker-release <command> [options]

Commands:
  watch               Start the controller (monitors Docker events, triggers deployments)
  rollback <service>  Roll back a service to the previous deployment
  status [service]    Show current deployment state
  version             Print version
  help                Show this help

Environment:
  RELEASE_STATE_DIR   Directory for state files (default: /var/lib/docker-release)
  RELEASE_CONFIG_DIR  Directory for provider config files (default: /etc/docker-release/conf.d)

`, version)
}
