package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/malico/docker-release/internal/config"
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
	case "release":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: dr release <service> [--force]")
			os.Exit(1)
		}
		if os.Args[2] == "--help" || os.Args[2] == "-h" {
			printUsage()
			return
		}
		runRelease(os.Args[2], len(os.Args) >= 4 && os.Args[3] == "--force")
	case "rollback":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: dr rollback <service>")
			os.Exit(1)
		}
		if os.Args[2] == "--help" || os.Args[2] == "-h" {
			printUsage()
			return
		}
		run(func(ctrl *controller.Controller) error {
			return ctrl.Rollback(context.Background(), os.Args[2])
		})
	case "status":
		if len(os.Args) >= 3 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printUsage()
			return
		}
		service := ""
		if len(os.Args) >= 3 {
			service = os.Args[2]
		}
		run(func(ctrl *controller.Controller) error {
			return ctrl.Status(context.Background(), service)
		})
	case "version":
		fmt.Printf("dr %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		// Positional shorthand: dr <service> [--force]
		// Anything not matching a reserved command is treated as a service name.
		// If your service is named after a reserved word, use: dr release <service>
		runRelease(os.Args[1], len(os.Args) >= 3 && os.Args[2] == "--force")
	}
}

func runRelease(service string, force bool) {
	run(func(ctrl *controller.Controller) error {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		if err := ctrl.Release(ctx, service, force); err != nil {
			return err
		}

		ctrl.WaitDeployments()
		return nil
	})
}

func run(fn func(*controller.Controller) error) {
	dockerClient, err := docker.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer dockerClient.Close()

	project, err := config.DetectProject(context.Background(), dockerClient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot determine compose project name: %v\n", err)
		os.Exit(1)
	}
	log.Printf("compose project: %s", project)

	mgr := state.NewManager("/var/lib/docker-release", project)
	ctrl := controller.New(dockerClient, mgr, project)

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

func printUsage() {
	fmt.Printf(`dr %s — deployment controller for Docker Compose

Usage:
  dr <service> [--force]           Deploy a service (short form)
  dr <command> [options]

Commands:
  <service>                        Deploy the named service (alias for release)
  release <service> [--force]      Deploy a service explicitly
                                   --force overrides an in-progress deployment
  rollback <service>               Roll back a service to its previous deployment
  status [service]                 Show deployment state
  watch                            Start the controller (run via compose, not manually)
  version                          Print version
  help, --help, -h                 Show this help

Note: if a service name collides with a reserved command (e.g. a service named
"status"), use the explicit form: dr release status

`, version)
}
