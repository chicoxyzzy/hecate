package storage

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

var ErrNil = errors.New("redis nil reply")

type RedisConfig struct {
	Address  string
	Password string
	DB       int
	Timeout  time.Duration
}

type RedisClient struct {
	config RedisConfig
}

func NewRedisClient(cfg RedisConfig) *RedisClient {
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Second
	}
	return &RedisClient{config: cfg}
}

func (c *RedisClient) Get(ctx context.Context, key string) ([]byte, error) {
	reply, err := c.run(ctx, "GET", key)
	if err != nil {
		return nil, err
	}
	value, ok := reply.([]byte)
	if !ok {
		return nil, fmt.Errorf("unexpected GET reply type %T", reply)
	}
	return value, nil
}

func (c *RedisClient) SetEX(ctx context.Context, key string, ttl time.Duration, value []byte) error {
	seconds := int(ttl / time.Second)
	if ttl > 0 && seconds == 0 {
		seconds = 1
	}

	args := []string{"SET", key, string(value)}
	if seconds > 0 {
		args = append(args, "EX", strconv.Itoa(seconds))
	}

	reply, err := c.run(ctx, args...)
	if err != nil {
		return err
	}
	status, ok := reply.(string)
	if !ok || status != "OK" {
		return fmt.Errorf("unexpected SET reply: %#v", reply)
	}
	return nil
}

func (c *RedisClient) Set(ctx context.Context, key string, value []byte) error {
	reply, err := c.run(ctx, "SET", key, string(value))
	if err != nil {
		return err
	}
	status, ok := reply.(string)
	if !ok || status != "OK" {
		return fmt.Errorf("unexpected SET reply: %#v", reply)
	}
	return nil
}

func (c *RedisClient) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	reply, err := c.run(ctx, "INCRBY", key, strconv.FormatInt(delta, 10))
	if err != nil {
		return 0, err
	}
	value, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected INCRBY reply type %T", reply)
	}
	return value, nil
}

func (c *RedisClient) run(ctx context.Context, args ...string) (any, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(c.config.Timeout))
	}

	if err := writeCommand(conn, args...); err != nil {
		return nil, err
	}
	return readReply(bufio.NewReader(conn))
}

func (c *RedisClient) dial(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: c.config.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.config.Address)
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	if c.config.Password != "" {
		if err := writeCommand(conn, "AUTH", c.config.Password); err != nil {
			conn.Close()
			return nil, err
		}
		if _, err := readReply(reader); err != nil {
			conn.Close()
			return nil, err
		}
	}
	if c.config.DB != 0 {
		if err := writeCommand(conn, "SELECT", strconv.Itoa(c.config.DB)); err != nil {
			conn.Close()
			return nil, err
		}
		if _, err := readReply(reader); err != nil {
			conn.Close()
			return nil, err
		}
	}

	return &bufferedConn{Conn: conn, reader: reader}, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func writeCommand(w io.Writer, args ...string) error {
	if _, err := fmt.Fprintf(w, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

func readReply(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	switch prefix {
	case '+':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		return line, nil
	case '-':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		return nil, errors.New(line)
	case ':':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		value, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, err
		}
		return value, nil
	case '$':
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size == -1 {
			return nil, ErrNil
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return buf[:size], nil
	default:
		return nil, fmt.Errorf("unsupported redis reply prefix %q", prefix)
	}
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
