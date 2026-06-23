package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

// colores para el output en terminal
var (
	colorOK      = color.New(color.FgGreen, color.Bold)
	colorErr     = color.New(color.FgRed, color.Bold)
	colorMuted   = color.New(color.FgHiBlack)
	colorSpinner = color.New(color.FgCyan)
)

// imprimirJSON serializa v como JSON indentado en stdout.
func imprimirJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v) //nolint
}

// imprimirError escribe el mensaje en stderr con color rojo y sale con código 1.
func imprimirError(msg string) {
	colorErr.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}

// imprimirTablaApps escribe una tabla formateada con la lista de apps en w.
func imprimirTablaApps(apps []appResp, w io.Writer) {
	tw := tablewriter.NewWriter(w)
	tw.SetHeader([]string{"Name", "Repo URL", "Created At"})
	tw.SetBorder(false)
	tw.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	tw.SetAlignment(tablewriter.ALIGN_LEFT)
	tw.SetColumnSeparator("  ")
	tw.SetHeaderLine(false)
	tw.SetNoWhiteSpace(true)

	for _, a := range apps {
		tw.Append([]string{a.Name, a.RepoURL, formatearFecha(a.CreatedAt)})
	}
	tw.Render()
}

// imprimirTablaDeployments escribe una tabla formateada con el histórico de deployments en w.
func imprimirTablaDeployments(deps []deploymentResp, w io.Writer) {
	tw := tablewriter.NewWriter(w)
	tw.SetHeader([]string{"ID", "Status", "Image Tag", "Created At"})
	tw.SetBorder(false)
	tw.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	tw.SetAlignment(tablewriter.ALIGN_LEFT)
	tw.SetColumnSeparator("  ")
	tw.SetHeaderLine(false)
	tw.SetNoWhiteSpace(true)

	for _, d := range deps {
		id := d.ID
		if len(id) >= 8 {
			id = id[:8]
		}
		tw.Append([]string{id, colorearStatus(d.Status), d.ImageTag, formatearFecha(d.CreatedAt)})
	}
	tw.Render()
}

// imprimirDetalleApp muestra los campos de una app con su deployment activo si existe.
func imprimirDetalleApp(a appResp) {
	fmt.Printf("Name:        %s\n", a.Name)
	fmt.Printf("ID:          %s\n", a.ID)
	fmt.Printf("Repo URL:    %s\n", a.RepoURL)
	fmt.Printf("Health Path: %s\n", a.HealthPath)
	fmt.Printf("Created At:  %s\n", formatearFecha(a.CreatedAt))
	if a.ActiveDeployment != nil {
		fmt.Println()
		fmt.Println("Active Deployment:")
		imprimirDetalleDeployment(*a.ActiveDeployment, "  ")
	} else {
		colorMuted.Println("\nNo active deployment.")
	}
}

// imprimirDetalleDeployment muestra los campos de un deployment con un prefijo de indentación.
func imprimirDetalleDeployment(d deploymentResp, prefix string) {
	fmt.Printf("%sID:        %s\n", prefix, d.ID)
	fmt.Printf("%sStatus:    %s\n", prefix, colorearStatus(d.Status))
	fmt.Printf("%sImage Tag: %s\n", prefix, d.ImageTag)
	fmt.Printf("%sCreated:   %s\n", prefix, formatearFecha(d.CreatedAt))
	if d.FinishedAt != nil {
		fmt.Printf("%sFinished:  %s\n", prefix, formatearFecha(*d.FinishedAt))
	}
	if d.ContainerID != nil {
		cid := *d.ContainerID
		if len(cid) > 12 {
			cid = cid[:12]
		}
		fmt.Printf("%sContainer: %s\n", prefix, cid)
	}
	if d.InternalPort != nil {
		fmt.Printf("%sPort:      %d\n", prefix, *d.InternalPort)
	}
	if d.ErrorMessage != nil && *d.ErrorMessage != "" {
		colorErr.Fprintf(os.Stdout, "%sError:     %s\n", prefix, *d.ErrorMessage)
	}
}

// colorearStatus retorna el status con color ANSI según su valor.
func colorearStatus(status string) string {
	switch strings.ToLower(status) {
	case "running":
		return colorOK.Sprint(status)
	case "failed":
		return colorErr.Sprint(status)
	default:
		return colorMuted.Sprint(status)
	}
}

// formatearFecha recorta el timestamp RFC3339 a los primeros 19 chars (sin zona horaria)
// para que quepa cómodamente en la tabla.
func formatearFecha(s string) string {
	if len(s) >= 19 {
		return s[:19]
	}
	return s
}
