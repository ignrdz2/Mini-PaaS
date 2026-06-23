package main

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// nuevoCmdDeployments construye el comando `deployments` con sus subcomandos.
func nuevoCmdDeployments() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployments",
		Short: "Gestionar deployments",
	}

	cmd.AddCommand(
		nuevoCmdDeploymentsList(),
		nuevoCmdDeploymentsGet(),
	)
	return cmd
}

// nuevoCmdDeploymentsList construye el subcomando `deployments list`.
func nuevoCmdDeploymentsList() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "list <app-name>",
		Short: "Listar el histórico de deployments de una app",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]
			c := newAPIClient(15 * time.Second)

			var deps []deploymentResp
			if err := c.hacer(context.Background(), "GET", "/apps/"+appName+"/deployments", nil, &deps); err != nil {
				imprimirError(err.Error())
			}

			if jsonOut {
				imprimirJSON(deps)
				return
			}

			if len(deps) == 0 {
				colorMuted.Println("No hay deployments para esta app.")
				return
			}
			imprimirTablaDeployments(deps, os.Stdout)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON puro")
	return cmd
}

// nuevoCmdDeploymentsGet construye el subcomando `deployments get`.
func nuevoCmdDeploymentsGet() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "get <app-name> <deployment-id>",
		Short: "Detalle de un deployment puntual",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]
			depID := args[1]
			c := newAPIClient(15 * time.Second)

			var dep deploymentResp
			path := "/apps/" + appName + "/deployments/" + depID
			if err := c.hacer(context.Background(), "GET", path, nil, &dep); err != nil {
				imprimirError(err.Error())
			}

			if jsonOut {
				imprimirJSON(dep)
				return
			}
			imprimirDetalleDeployment(dep, "")
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON puro")
	return cmd
}
