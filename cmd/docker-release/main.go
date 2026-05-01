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
		if len(os.Args) < 3 || os.Args[2] == "--help" || os.Args[2] == "-h" {
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: docker-release release <service> [--force]")
				os.Exit(1)
			}
			printUsage()
			return
		}
		service := os.Args[2]
		force := len(os.Args) >= 4 && os.Args[3] == "--force"
		run(func(ctrl *controller.Controller) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			if err := ctrl.Release(ctx, service, force); err != nil {
				return err
			}

			ctrl.WaitDeployments()
			return nil
		})
	case "rollback":
		if len(os.Args) < 3 || os.Args[2] == "--help" || os.Args[2] == "-h" {
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: docker-release rollback <service>")
				os.Exit(1)
			}
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

	project, err := detectProject(dockerClient)
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

func detectProject(dockerClient *docker.Client) (string, error) {
	return config.DetectProject(context.Background(), dockerClient)
}

func cmdWatch(ctrl *controller.Controller) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return ctrl.Watch(ctx)
}

func printUsage() {
	fmt.Printf(`dr %s — deployment controller for Docker Compose

Usage:
  dr <command> [options]

Commands:
  watch                        Start the controller (monitors Docker events)
  release <service> [--force]  Deploy a service
                               --force overrides an in-progress deployment
  rollback <service>           Roll back a service to its previous deployment
  status [service]             Show deployment state
  version                      Print version
  help, --help, -h             Show this help

`, version)
}
