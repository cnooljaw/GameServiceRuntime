package nhsk

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const customDeckRedisKeyPrefix = "game:makecard"

var (
	errInvalidRedisGetter = errors.New("nhsk: invalid Redis custom-deck getter")
	errRedisProtocol      = errors.New("nhsk: invalid Redis response")
)

// RedisStringGetter is the narrow Redis capability required by the custom
// deck provider. It deliberately exposes only GET so the provider cannot
// mutate Redis or make Battle depend on a Redis client package.
type RedisStringGetter interface {
	Get(context.Context, string) (value string, found bool, err error)
}

// RedisCustomDeckProvider reads the legacy MakecardConfig keys in their
// original priority order: ProductID first, then GameID only for an empty or
// missing ProductID value.
type RedisCustomDeckProvider struct {
	Getter RedisStringGetter
}

// Load reads and parses one Redis snapshot outside the Battle Mailbox.
func (provider RedisCustomDeckProvider) Load(ctx context.Context, lookup CustomDeckLookup) (CustomDeckCatalog, error) {
	if provider.Getter == nil {
		return CustomDeckCatalog{}, errInvalidRedisGetter
	}
	if err := contextError(ctx); err != nil {
		return CustomDeckCatalog{}, err
	}
	if lookup.ProductID != 0 {
		catalog, usable, err := provider.loadKey(ctx, lookup.ProductID)
		if err != nil || usable {
			return catalog, err
		}
	}
	if lookup.GameID == 0 {
		return CustomDeckCatalog{}, nil
	}
	return provider.loadGameKey(ctx, lookup.GameID)
}

func (provider RedisCustomDeckProvider) loadKey(ctx context.Context, id uint32) (CustomDeckCatalog, bool, error) {
	key := fmt.Sprintf("%s:%d", customDeckRedisKeyPrefix, id)
	value, found, err := provider.Getter.Get(ctx, key)
	if err != nil {
		return CustomDeckCatalog{}, true, err
	}
	if !found || strings.TrimSpace(value) == "" {
		return CustomDeckCatalog{}, false, nil
	}
	catalog, err := ParseCustomDeck(value)
	return catalog, true, err
}

func (provider RedisCustomDeckProvider) loadGameKey(ctx context.Context, id uint32) (CustomDeckCatalog, error) {
	catalog, _, err := provider.loadKey(ctx, id)
	return catalog, err
}

// TCPRedisStringGetter is a dependency-free RESP2 GET adapter. It opens one
// bounded connection per GET, authenticates/selects the configured database,
// and closes the connection before returning. A production pool can implement
// RedisStringGetter without changing RedisCustomDeckProvider or Battle.
type TCPRedisStringGetter struct {
	Address       string
	Password      string
	DB            int
	DialTimeout   time.Duration
	MaxValueBytes int64
	// Dial optionally replaces the standard TCP dialer for tests or an
	// application-owned connection transport.
	Dial func(context.Context, string) (net.Conn, error)
}

// Get executes one Redis GET and returns found=false for a Redis nil bulk
// string. Authentication and database selection are performed when configured.
func (getter TCPRedisStringGetter) Get(ctx context.Context, key string) (string, bool, error) {
	if strings.TrimSpace(getter.Address) == "" || strings.TrimSpace(key) == "" || getter.DB < 0 {
		return "", false, errInvalidRedisGetter
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dialTimeout := getter.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	maxValueBytes := getter.MaxValueBytes
	if maxValueBytes <= 0 {
		maxValueBytes = 1 << 20
	}
	var connection net.Conn
	var err error
	if getter.Dial != nil {
		connection, err = getter.Dial(ctx, getter.Address)
	} else {
		dialer := net.Dialer{Timeout: dialTimeout}
		connection, err = dialer.DialContext(ctx, "tcp", getter.Address)
	}
	if err != nil {
		return "", false, err
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	} else {
		_ = connection.SetDeadline(time.Now().Add(dialTimeout))
	}
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	if getter.Password != "" {
		if err := writeRESPCommand(writer, "AUTH", getter.Password); err != nil {
			return "", false, err
		}
		if err := readRedisStatus(reader); err != nil {
			return "", false, err
		}
	}
	if getter.DB != 0 {
		if err := writeRESPCommand(writer, "SELECT", strconv.Itoa(getter.DB)); err != nil {
			return "", false, err
		}
		if err := readRedisStatus(reader); err != nil {
			return "", false, err
		}
	}
	if err := writeRESPCommand(writer, "GET", key); err != nil {
		return "", false, err
	}
	return readRedisBulk(reader, maxValueBytes)
}

func writeRESPCommand(writer *bufio.Writer, args ...string) error {
	if _, err := fmt.Fprintf(writer, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(writer, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func readRedisStatus(reader *bufio.Reader) error {
	line, err := readRedisLine(reader)
	if err != nil {
		return err
	}
	if len(line) == 0 {
		return errRedisProtocol
	}
	switch line[0] {
	case '+':
		return nil
	case '-':
		return fmt.Errorf("redis command failed: %s", line[1:])
	default:
		return errRedisProtocol
	}
}

func readRedisBulk(reader *bufio.Reader, maxValueBytes int64) (string, bool, error) {
	line, err := readRedisLine(reader)
	if err != nil {
		return "", false, err
	}
	if len(line) == 0 {
		return "", false, errRedisProtocol
	}
	if line[0] == '-' {
		return "", false, fmt.Errorf("redis GET failed: %s", line[1:])
	}
	if line[0] != '$' {
		return "", false, errRedisProtocol
	}
	length, err := strconv.ParseInt(line[1:], 10, 64)
	if err != nil || length < -1 {
		return "", false, errRedisProtocol
	}
	if length == -1 {
		return "", false, nil
	}
	if length > maxValueBytes {
		return "", false, fmt.Errorf("redis value exceeds limit: %d", length)
	}
	value := make([]byte, length+2)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", false, err
	}
	if value[length] != '\r' || value[length+1] != '\n' {
		return "", false, errRedisProtocol
	}
	return string(value[:length]), true, nil
}

func readRedisLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 2 || line[len(line)-2:] != "\r\n" {
		return "", errRedisProtocol
	}
	return line[:len(line)-2], nil
}
