package main

import (
	"os"

	"github.com/lazaroagomez/wusbkit/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
