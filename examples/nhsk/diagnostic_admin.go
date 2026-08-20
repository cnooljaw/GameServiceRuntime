package nhsk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lijiawang/GameServiceRuntime/game"
	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

const diagnosticAdminMaxMessageBytes = 64 * 1024

var errInvalidDiagnosticAdmin = errors.New("nhsk: invalid diagnostic admin request")

type diagnosticAdminRequest struct {
	Operation string
	BattleID  game.BattleID
	Ref       gsr.ServiceRef
	Receipt   DiagnosticReceipt
}

type diagnosticAdminResponse struct {
	Quarantined []QuarantinedBattleSnapshot
	ErrorCode   string
	Error       string
}

// DiagnosticAdminServer exposes only quarantine operations on a node-local Unix socket.
type DiagnosticAdminServer struct {
	path      string
	admin     *QuarantineAdmin
	mu        sync.Mutex
	listener  net.Listener
	done      chan struct{}
	closeOnce sync.Once
}

// NewDiagnosticAdminServer creates a stopped node-local admin owner.
func NewDiagnosticAdminServer(path string, admin *QuarantineAdmin) (*DiagnosticAdminServer, error) {
	if strings.TrimSpace(path) == "" || admin == nil || admin.runtime == nil || admin.host.ID == 0 {
		return nil, errInvalidDiagnosticAdmin
	}
	return &DiagnosticAdminServer{path: path, admin: admin, done: make(chan struct{})}, nil
}

// Start acquires the configured Unix socket and starts one tracked sequential I/O owner.
func (server *DiagnosticAdminServer) Start() error {
	if server == nil {
		return errInvalidDiagnosticAdmin
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.listener != nil {
		return errInvalidDiagnosticAdmin
	}
	if err := os.MkdirAll(filepath.Dir(server.path), 0o750); err != nil {
		return err
	}
	listener, err := net.Listen("unix", server.path)
	if err != nil {
		return err
	}
	if err := os.Chmod(server.path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(server.path)
		return err
	}
	server.listener = listener
	go server.serve(listener)
	return nil
}

// Close stops accepting admin requests, waits for the owner, and removes its socket file.
func (server *DiagnosticAdminServer) Close() error {
	if server == nil {
		return nil
	}
	var closeErr error
	server.closeOnce.Do(func() {
		server.mu.Lock()
		listener := server.listener
		server.mu.Unlock()
		if listener == nil {
			close(server.done)
			return
		}
		closeErr = listener.Close()
		<-server.done
		removeErr := os.Remove(server.path)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		closeErr = errors.Join(closeErr, removeErr)
	})
	return closeErr
}

func (server *DiagnosticAdminServer) serve(listener net.Listener) {
	defer close(server.done)
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		server.handle(connection)
	}
}

func (server *DiagnosticAdminServer) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	decoder := json.NewDecoder(io.LimitReader(connection, diagnosticAdminMaxMessageBytes))
	var request diagnosticAdminRequest
	if err := decoder.Decode(&request); err != nil {
		_ = json.NewEncoder(connection).Encode(diagnosticAdminError(errInvalidDiagnosticAdmin))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response := server.execute(ctx, request)
	_ = json.NewEncoder(connection).Encode(response)
}

func (server *DiagnosticAdminServer) execute(ctx context.Context, request diagnosticAdminRequest) diagnosticAdminResponse {
	switch request.Operation {
	case "list":
		entries, err := server.admin.ListQuarantined(ctx)
		if err != nil {
			return diagnosticAdminError(err)
		}
		return diagnosticAdminResponse{Quarantined: entries}
	case "retry":
		return diagnosticAdminError(server.admin.RetryDiagnostic(ctx, request.BattleID, request.Ref))
	case "release":
		return diagnosticAdminError(server.admin.ReleaseQuarantinedBattle(ctx, request.Receipt))
	case "cleanup":
		return diagnosticAdminError(server.admin.CleanupDiagnosticMaterial(request.Receipt))
	default:
		return diagnosticAdminError(errInvalidDiagnosticAdmin)
	}
}

func diagnosticAdminError(err error) diagnosticAdminResponse {
	if err == nil {
		return diagnosticAdminResponse{}
	}
	response := diagnosticAdminResponse{Error: err.Error(), ErrorCode: "operation_failed"}
	if errors.Is(err, ErrDiagnosticReceiptMismatch) {
		response.ErrorCode = "receipt_mismatch"
	}
	return response
}

// DiagnosticAdminClient invokes one running node through its local Unix socket.
type DiagnosticAdminClient struct{ path string }

// NewDiagnosticAdminClient binds a client to one local socket path.
func NewDiagnosticAdminClient(path string) *DiagnosticAdminClient {
	return &DiagnosticAdminClient{path: path}
}

// ListQuarantined lists retained entries from the live GameLogic process.
func (client *DiagnosticAdminClient) ListQuarantined(ctx context.Context) ([]QuarantinedBattleSnapshot, error) {
	response, err := client.call(ctx, diagnosticAdminRequest{Operation: "list"})
	return response.Quarantined, err
}

// RetryDiagnostic retries evidence export for one exact entry.
func (client *DiagnosticAdminClient) RetryDiagnostic(ctx context.Context, battleID game.BattleID, ref gsr.ServiceRef) error {
	_, err := client.call(ctx, diagnosticAdminRequest{Operation: "retry", BattleID: battleID, Ref: ref})
	return err
}

// ReleaseQuarantinedBattle releases one exact entry using its published receipt.
func (client *DiagnosticAdminClient) ReleaseQuarantinedBattle(ctx context.Context, receipt DiagnosticReceipt) error {
	_, err := client.call(ctx, diagnosticAdminRequest{Operation: "release", Receipt: receipt})
	return err
}

// CleanupDiagnosticMaterial removes one receipt-bound local material directory.
func (client *DiagnosticAdminClient) CleanupDiagnosticMaterial(ctx context.Context, receipt DiagnosticReceipt) error {
	_, err := client.call(ctx, diagnosticAdminRequest{Operation: "cleanup", Receipt: receipt})
	return err
}

func (client *DiagnosticAdminClient) call(ctx context.Context, request diagnosticAdminRequest) (diagnosticAdminResponse, error) {
	if client == nil || strings.TrimSpace(client.path) == "" {
		return diagnosticAdminResponse{}, errInvalidDiagnosticAdmin
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", client.path)
	if err != nil {
		return diagnosticAdminResponse{}, err
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return diagnosticAdminResponse{}, err
	}
	var response diagnosticAdminResponse
	if err := json.NewDecoder(io.LimitReader(connection, diagnosticAdminMaxMessageBytes)).Decode(&response); err != nil {
		return diagnosticAdminResponse{}, err
	}
	if response.Error == "" {
		return response, nil
	}
	if response.ErrorCode == "receipt_mismatch" {
		return response, ErrDiagnosticReceiptMismatch
	}
	return response, errors.New(response.Error)
}
