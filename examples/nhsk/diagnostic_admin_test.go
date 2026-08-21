package nhsk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

func TestDiagnosticAdminSocketListsAndPreciselyReleasesQuarantine(t *testing.T) {
	fixture := newQuarantineHostFixture(t, 1)
	ref := fixture.create(t, 81, 14)
	fixture.quarantine(t, 81, ref, 14)
	receipt := DiagnosticReceipt{ReceiptID: "receipt-81", BattleID: 81, Ref: ref, Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Directory: t.TempDir(), CreatedAt: time.Unix(800, 0)}
	if err := fixture.runtime.Send(fixture.factory, reportBattleQuarantinedCommand, battleQuarantineReport{BattleID: 81, Ref: ref, ConnectionGeneration: 14, Evidence: testDiagnosticArtifact().Evidence}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runtime.Send(fixture.host, applyDiagnosticExportResultCommand, gsr.RunnerResult[diagnosticExportResult]{Value: diagnosticExportResult{BattleID: 81, Ref: ref, Receipt: receipt}}); err != nil {
		t.Fatal(err)
	}

	socket := shortAdminSocket(t)
	server, err := NewDiagnosticAdminServer(socket, NewQuarantineAdmin(fixture.runtime, fixture.host, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client := NewDiagnosticAdminClient(socket)

	entries, err := client.ListQuarantined(context.Background())
	if err != nil || len(entries) != 1 || entries[0].BattleID != 81 {
		t.Fatalf("list = %#v, %v", entries, err)
	}
	wrong := receipt
	wrong.Ref = gsr.ServiceRef{Node: ref.Node, ID: ref.ID + 1}
	if err := client.ReleaseQuarantinedBattle(context.Background(), wrong); !errors.Is(err, ErrDiagnosticReceiptMismatch) {
		t.Fatalf("wrong release error = %v", err)
	}
	if err := client.ReleaseQuarantinedBattle(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticAdminSocketRejectsSecondOwnerAndClosesCleanly(t *testing.T) {
	fixture := newQuarantineHostFixture(t, 1)
	socket := shortAdminSocket(t)
	server, err := NewDiagnosticAdminServer(socket, NewQuarantineAdmin(fixture.runtime, fixture.host, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	second, err := NewDiagnosticAdminServer(socket, NewQuarantineAdmin(fixture.runtime, fixture.host, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err == nil {
		t.Fatal("second server unexpectedly acquired active socket")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func shortAdminSocket(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	directory = filepath.Clean(filepath.Join(directory, "..", ".."))
	path := filepath.Join(directory, fmt.Sprintf(".na-%d-%d.sock", os.Getpid(), time.Now().UnixNano()%1_000_000))
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}
