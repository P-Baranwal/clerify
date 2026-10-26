package main

import (
	"os"

	"github.com/P-Baranwal/clerify/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
