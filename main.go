package main

import (
	"github.com/omecodes/app-registry/cmd"
	"github.com/omecodes/common/utils/log"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal("command execution failed", log.Err(err))
	}
}
