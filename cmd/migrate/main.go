package main

import (
	"fmt"
	"os"

	"github.com/graphic/gofhir/internal/auditor"
)

func main() {
	path := "data/gofhir.db"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	s, err := auditor.Open(path)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()
	fmt.Printf("migration ok: %s\n", path)
}