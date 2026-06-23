package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// nuevoCmdApps construye el comando `apps` con todos sus subcomandos.
func nuevoCmdApps() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Gestionar aplicaciones",
	}

	cmd.AddCommand(
		nuevoCmdAppsCreate(),
		nuevoCmdAppsList(),
		nuevoCmdAppsGet(),
		nuevoCmdAppsDelete(),
		nuevoCmdAppsDeploy(),
	)
	return cmd
}

// nuevoCmdAppsCreate construye el subcomando `apps create`.
func nuevoCmdAppsCreate() *cobra.Command {
	var repoURL string
	var healthPath string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Crear una nueva aplicación",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if healthPath == "" {
				healthPath = "/"
			}

			c := newAPIClient(15 * time.Second)
			body := map[string]string{
				"name":        name,
				"repo_url":    repoURL,
				"health_path": healthPath,
			}

			var app appResp
			if err := c.hacer(context.Background(), "POST", "/apps", body, &app); err != nil {
				imprimirError(err.Error())
			}

			color.New(color.FgGreen, color.Bold).Printf("✓ App %q creada (id: %s)\n", app.Name, app.ID[:8])
		},
	}

	cmd.Flags().StringVar(&repoURL, "repo", "", "URL del repositorio Git (requerido)")
	cmd.Flags().StringVar(&healthPath, "health-path", "/", "Path del healthcheck")
	cmd.MarkFlagRequired("repo") //nolint
	return cmd
}

// nuevoCmdAppsList construye el subcomando `apps list`.
func nuevoCmdAppsList() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Listar todas las aplicaciones",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			c := newAPIClient(15 * time.Second)

			var apps []appResp
			if err := c.hacer(context.Background(), "GET", "/apps", nil, &apps); err != nil {
				imprimirError(err.Error())
			}

			if jsonOut {
				imprimirJSON(apps)
				return
			}

			if len(apps) == 0 {
				colorMuted.Println("No hay apps registradas.")
				return
			}
			imprimirTablaApps(apps, os.Stdout)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON puro")
	return cmd
}

// nuevoCmdAppsGet construye el subcomando `apps get`.
func nuevoCmdAppsGet() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Detalle de una app y su deployment activo",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			c := newAPIClient(15 * time.Second)

			var app appResp
			if err := c.hacer(context.Background(), "GET", "/apps/"+name, nil, &app); err != nil {
				imprimirError(err.Error())
			}

			if jsonOut {
				imprimirJSON(app)
				return
			}
			imprimirDetalleApp(app)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON puro")
	return cmd
}

// nuevoCmdAppsDelete construye el subcomando `apps delete`.
func nuevoCmdAppsDelete() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Eliminar una app y detener su deployment activo",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			c := newAPIClient(15 * time.Second)

			if err := c.hacer(context.Background(), "DELETE", "/apps/"+name, nil, nil); err != nil {
				imprimirError(err.Error())
			}

			color.New(color.FgGreen, color.Bold).Printf("✓ App %q eliminada\n", name)
		},
	}

	return cmd
}

// nuevoCmdAppsDeploy construye el subcomando `apps deploy`.
func nuevoCmdAppsDeploy() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy <name>",
		Short: "Disparar un deploy de la app (síncrono, espera hasta 10 minutos)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			// sin timeout en el cliente — el orquestador gestiona su propio timeout de 10 min
			c := newAPIClientSinTimeout()

			colorSpinner.Printf("⟳ Desplegando %q... (esto puede tardar varios minutos)\n", name)

			var dep deploymentResp
			if err := c.hacer(context.Background(), "POST", "/apps/"+name+"/deployments", map[string]any{}, &dep); err != nil {
				imprimirError(err.Error())
			}

			fmt.Println()
			switch dep.Status {
			case "running":
				color.New(color.FgGreen, color.Bold).Printf("✓ Deploy exitoso — status: %s\n", dep.Status)
			case "failed":
				color.New(color.FgRed, color.Bold).Printf("✗ Deploy fallido — status: %s\n", dep.Status)
				if dep.ErrorMessage != nil && *dep.ErrorMessage != "" {
					fmt.Fprintf(os.Stderr, "\nError:\n%s\n", *dep.ErrorMessage)
				}
				os.Exit(1)
			default:
				fmt.Printf("Status final: %s\n", dep.Status)
			}

			fmt.Printf("ID:        %s\n", dep.ID)
			fmt.Printf("Image Tag: %s\n", dep.ImageTag)
			if dep.InternalPort != nil {
				fmt.Printf("Puerto:    %d\n", *dep.InternalPort)
			}
		},
	}

	return cmd
}
