package nhsk

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRedisCustomDeckProviderIntegratesWithRedisServer(t *testing.T) {
	redisServer, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}

	command := exec.Command(redisServer,
		"--bind", "127.0.0.1",
		"--port", port,
		"--save", "",
		"--appendonly", "no",
		"--dir", t.TempDir(),
	)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		_ = command.Wait()
	})

	getter := TCPRedisStringGetter{Address: address, DialTimeout: 100 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, _, err = getter.Get(context.Background(), "nhsk:ready")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Redis did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	value := "{\n@2\n" + sequentialCustomDeckLine() + "\n}\n"
	if err := setRedisIntegrationValue(address, "game:makecard:100", value); err != nil {
		t.Fatal(err)
	}
	provider := RedisCustomDeckProvider{Getter: getter}
	catalog, err := provider.Load(context.Background(), CustomDeckLookup{GameID: 82, ProductID: 100})
	if err != nil || len(catalog.Decks) != 1 || catalog.Decks[0].BankerSeat != 2 || len(catalog.Decks[0].Cards) != 104 {
		t.Fatalf("real Redis catalog = %#v, err=%v", catalog, err)
	}
}

func setRedisIntegrationValue(address, key, value string) error {
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	request := fmt.Sprintf("*3\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n", len(key), key, len(value), value)
	if _, err := io.WriteString(connection, request); err != nil {
		return err
	}
	reply, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		return err
	}
	if strings.TrimSpace(reply) != "+OK" {
		return fmt.Errorf("Redis SET reply %q from %q", reply, address)
	}
	return nil
}
