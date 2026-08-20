package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lijiawang/GameServiceRuntime/examples/nhsk"
	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func main() {
	socket := flag.String("socket", "diagnostics/nhsk-admin.sock", "running NHSK GameLogic diagnostic Unix socket")
	operation := flag.String("op", "list", "list, retry, release, or cleanup")
	battleID := flag.Uint("battle", 0, "BattleID for retry")
	refNode := flag.String("ref-node", "", "ServiceRef node for retry")
	refID := flag.Uint64("ref-id", 0, "ServiceRef ID for retry")
	receiptPath := flag.String("receipt", "", "receipt.json path for release or cleanup")
	timeout := flag.Duration("timeout", 5*time.Second, "operation timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client := nhsk.NewDiagnosticAdminClient(*socket)
	switch *operation {
	case "list":
		entries, err := client.ListQuarantined(ctx)
		if err != nil {
			log.Fatal(err)
		}
		printJSON(entries)
	case "retry":
		if *battleID == 0 || *refNode == "" || *refID == 0 {
			log.Fatal("retry requires -battle, -ref-node, and -ref-id")
		}
		if err := client.RetryDiagnostic(ctx, game.BattleID(*battleID), gsr.ServiceRef{Node: gsr.NodeID(*refNode), ID: gsr.ServiceID(*refID)}); err != nil {
			log.Fatal(err)
		}
		fmt.Println("diagnostic retry accepted")
	case "release", "cleanup":
		receipt, err := readReceipt(*receiptPath)
		if err != nil {
			log.Fatal(err)
		}
		if *operation == "release" {
			err = client.ReleaseQuarantinedBattle(ctx, receipt)
		} else {
			err = client.CleanupDiagnosticMaterial(ctx, receipt)
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("diagnostic %s accepted\n", *operation)
	default:
		log.Fatalf("unsupported operation %q", *operation)
	}
}

func readReceipt(path string) (nhsk.DiagnosticReceipt, error) {
	if path == "" {
		return nhsk.DiagnosticReceipt{}, fmt.Errorf("-receipt is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nhsk.DiagnosticReceipt{}, err
	}
	var receipt nhsk.DiagnosticReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nhsk.DiagnosticReceipt{}, err
	}
	return receipt, nil
}

func printJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		log.Fatal(err)
	}
}
