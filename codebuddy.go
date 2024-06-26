package main

import (
	"log"

	"github.com/psanford/code-buddy/cmd"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		log.Fatal(err)
	}
}
