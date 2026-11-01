// stackit-ws-manager automates STACKIT workshop account creation.
package main

import (
	"os"

	"github.com/denktmit/stackit-ws-manager/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
