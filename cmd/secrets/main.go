package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/graphic/gofhir/internal/secrets"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	secretsFile := os.Getenv("GOFHIR_SECRETS_FILE")
	if secretsFile == "" {
		secretsFile = "~/.gofhir/secrets.enc"
	}

	cmd := os.Args[1]

	switch cmd {
	case "init":
		handleInit(secretsFile)
	case "list":
		handleList(secretsFile)
	case "get":
		handleGet(secretsFile)
	case "set":
		handleSet(secretsFile)
	case "rotate":
		handleRotate(secretsFile)
	case "rotate-all":
		handleRotateAll(secretsFile)
	case "export":
		handleExport(secretsFile)
	case "validate":
		handleValidate(secretsFile)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("GoFHIR Secrets Management Tool")
	fmt.Println("")
	fmt.Println("Usage: gofhir-secrets <command> [args]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  init                 Initialize new secrets file")
	fmt.Println("  list                 List all secrets (masked)")
	fmt.Println("  get <KEY>            Get specific secret")
	fmt.Println("  set <KEY> <VALUE>    Set specific secret")
	fmt.Println("  rotate <KEY>         Rotate specific secret")
	fmt.Println("  rotate-all           Rotate all secrets")
	fmt.Println("  export               Export as env vars")
	fmt.Println("  validate             Validate required secrets exist")
	fmt.Println("  help                 Show this help message")
	fmt.Println("")
	fmt.Println("Environment Variables:")
	fmt.Println("  GOFHIR_SECRETS_FILE  Path to secrets file (default: ~/.gofhir/secrets.enc)")
	fmt.Println("  GOFHIR_MASTER_KEY     Master key for encryption (64 hex chars)")
}

func handleInit(secretsFile string) {
	var masterKey string
	if len(os.Args) > 2 {
		masterKey = os.Args[2]
	}

	if err := secrets.InitSecrets(secretsFile, masterKey); err != nil {
		fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Secrets file initialized: %s\n", secretsFile)
	fmt.Println("Master key saved to ~/.gofhir/master.key")
	fmt.Println("")
	fmt.Println("Required secrets (generate with 'rotate-all' or set manually):")
	fmt.Println("  GOFHIR_AUDIT_HMAC_KEY")
	fmt.Println("  GOFHIR_JWT_SECRET")
}

func handleList(secretsFile string) {
	mgr, err := secrets.NewManager(secretsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load secrets: %v\n", err)
		os.Exit(1)
	}

	secretsList := mgr.ToList()
	if len(secretsList) == 0 {
		fmt.Println("No secrets found")
		return
	}

	fmt.Println("Secrets:")
	for key, masked := range secretsList {
		fmt.Printf("  %s = %s\n", key, masked)
	}
}

func handleGet(secretsFile string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: gofhir-secrets get <KEY>")
		os.Exit(1)
	}

	key := os.Args[2]

	mgr, err := secrets.NewManager(secretsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load secrets: %v\n", err)
		os.Exit(1)
	}

	value, err := mgr.Get(key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get secret: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(value)
}

func handleSet(secretsFile string) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: gofhir-secrets set <KEY> <VALUE>")
		os.Exit(1)
	}

	key := os.Args[2]
	value := os.Args[3]

	mgr, err := secrets.NewManager(secretsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Load(); err != nil {
		if err == secrets.ErrSecretsFileNotFound {
			fmt.Fprintf(os.Stderr, "secrets file not found, run 'init' first\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "failed to load secrets: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Set(key, value); err != nil {
		fmt.Fprintf(os.Stderr, "failed to set secret: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Secret %s updated\n", key)
}

func handleRotate(secretsFile string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: gofhir-secrets rotate <KEY>")
		os.Exit(1)
	}

	key := os.Args[2]

	mgr, err := secrets.NewManager(secretsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load secrets: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Rotate(key); err != nil {
		fmt.Fprintf(os.Stderr, "failed to rotate secret: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Secret %s rotated\n", key)
}

func handleRotateAll(secretsFile string) {
	mgr, err := secrets.NewManager(secretsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Load(); err != nil {
		if err == secrets.ErrSecretsFileNotFound {
			fmt.Fprintf(os.Stderr, "secrets file not found, run 'init' first\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "failed to load secrets: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.RotateAll(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to rotate secrets: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All secrets rotated")
}

func handleExport(secretsFile string) {
	mgr, err := secrets.NewManager(secretsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load secrets: %v\n", err)
		os.Exit(1)
	}

	envVars, err := mgr.ExportToEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to export: %v\n", err)
		os.Exit(1)
	}

	for key, value := range envVars {
		if strings.Contains(value, " ") {
			fmt.Printf("export %s=\"%s\"\n", key, value)
		} else {
			fmt.Printf("export %s=%s\n", key, value)
		}
	}
}

func handleValidate(secretsFile string) {
	mgr, err := secrets.NewManager(secretsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load secrets: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All required secrets present")
}
