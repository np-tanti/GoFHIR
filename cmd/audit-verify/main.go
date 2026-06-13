package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/graphic/gofhir/internal/auditor"
)

func main() {
	dbPath := flag.String("db", "data/gofhir.db", "path to audit database")
	hmacKeyHex := flag.String("key", "", "HMAC key (hex)")
	flag.Parse()

	if *hmacKeyHex == "" {
		_, _ = fmt.Fprintf(os.Stderr, "error: --key is required\n")
		os.Exit(1)
	}

	key, err := auditor.HMACKeyFromHex(*hmacKeyHex)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: invalid hex key: %v\n", err)
		os.Exit(1)
	}

	store, err := auditor.Open(*dbPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()
	entries, err := store.ReadAll(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: read all: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("audit log is empty")
		return
	}

	brokenAt := auditor.VerifyChain(entries, key)
	if brokenAt == -1 {
		fmt.Printf("OK: chain intact, %d entries verified\n", len(entries))
		return
	}

	_, _ = fmt.Fprintf(os.Stderr, "CHAIN BROKEN at entry %d (seq=%d, action=%q, actor=%q)\n",
		brokenAt, entries[brokenAt].Seq, entries[brokenAt].Action, entries[brokenAt].ActorID)
	if brokenAt > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "  previous entry: seq=%d, action=%q\n",
			entries[brokenAt-1].Seq, entries[brokenAt-1].Action)
	}
	os.Exit(2)
}
