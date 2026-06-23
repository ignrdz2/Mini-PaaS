package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "deployctl",
		Short: "CLI para gestionar apps y deployments en Mini-PaaS",
		// silenciar el uso automático de cobra en errores — imprimirError lo maneja
		SilenceUsage: true,
	}

	root.AddCommand(
		nuevoCmdApps(),
		nuevoCmdDeployments(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
