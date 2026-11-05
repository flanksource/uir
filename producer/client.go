package producer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/uir/indexer"
)

// Protocol is the producer protocol version `initialize` must report.
const Protocol = "uir-producer/1"

const (
	stderrTailBytes = 4096
	killWaitDelay   = 2 * time.Second
)

// Info is the producer's `initialize` result.
type Info struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Protocol string   `json:"protocol"`
	Methods  []string `json:"methods"`
}

// RPCError is a JSON-RPC error object returned by the producer.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if len(e.Data) == 0 || string(e.Data) == "null" {
		return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("json-rpc error %d: %s (%s)", e.Code, e.Message, e.Data)
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

// Client is a connected producer child process.
type Client struct {
	binary Binary
	info   Info
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *tailBuffer
	nextID int
	done   bool
}

// Connect starts `<binary> uir`, sends initialize and verifies the protocol version and that the
// producer offers `publication`. The caller must Close the client unless Publication succeeded.
func Connect(ctx context.Context, binary Binary) (*Client, error) {
	cmd := exec.CommandContext(ctx, binary.Path, "uir")
	cmd.WaitDelay = killWaitDelay
	stderr := &tailBuffer{limit: stderrTailBytes}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("producer %s: open stdin: %w", binary.Path, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("producer %s: open stdout: %w", binary.Path, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("producer %s: start: %w", binary.Path, err)
	}
	client := &Client{binary: binary, cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr}
	if err := client.initialize(ctx); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) initialize(ctx context.Context) error {
	raw, err := c.call(ctx, "initialize", nil)
	if err != nil {
		return err
	}
	if err := decodeStrict(raw, &c.info); err != nil {
		return c.failure("decode the initialize result: %v", err)
	}
	if c.info.Protocol != Protocol {
		return c.failure("producer speaks protocol %q, uir needs %q; upgrade the producer (or uir) so both use %s", c.info.Protocol, Protocol, Protocol)
	}
	if !slices.Contains(c.info.Methods, "publication") {
		return c.failure("producer %q (version %s) does not offer the publication method (offers %v); upgrade it", c.info.Name, c.info.Version, c.info.Methods)
	}
	return nil
}

// Run connects to binary, fetches the publication of the checkout at path and stops the producer.
func Run(ctx context.Context, binary Binary, request PublicationRequest) (indexer.Publication, error) {
	client, err := Connect(ctx, binary)
	if err != nil {
		return indexer.Publication{}, err
	}
	defer client.Close()
	return client.Publication(ctx, request.Path, request.Security)
}

// Info is what the producer reported at initialize.
func (c *Client) Info() Info { return c.info }

// PublicationRequest is the `publication` method's params.
type PublicationRequest struct {
	Path     string `json:"path"`
	Security bool   `json:"security"`
}

// Publication asks the producer for the publication of a checkout, then shuts it down and waits for
// it to exit. An unset Options.Producer is filled with the producer's fingerprint.
func (c *Client) Publication(ctx context.Context, path string, security bool) (indexer.Publication, error) {
	raw, err := c.call(ctx, "publication", PublicationRequest{Path: path, Security: security})
	if err != nil {
		return indexer.Publication{}, err
	}
	var publication indexer.Publication
	if err := decodeStrict(raw, &publication); err != nil {
		return indexer.Publication{}, c.failure("decode the publication: %v", err)
	}
	if publication.Options.Producer == "" {
		if publication.Options.Producer, err = c.fingerprint(); err != nil {
			return indexer.Publication{}, err
		}
	}
	return publication, c.shutdown(ctx)
}

func (c *Client) fingerprint() (string, error) {
	sum, err := fileSHA256(c.binary.Path)
	if err != nil {
		return "", fmt.Errorf("fingerprint producer %s: %w", c.binary.Path, err)
	}
	return fmt.Sprintf("%s/%s+sha256:%s", c.info.Name, c.info.Version, sum), nil
}

func (c *Client) shutdown(ctx context.Context) error {
	if _, err := c.call(ctx, "shutdown", nil); err != nil {
		return err
	}
	c.done = true
	_ = c.stdin.Close()
	if err := c.cmd.Wait(); err != nil {
		return c.failure("producer did not exit cleanly after shutdown: %v", err)
	}
	return nil
}

// Close stops the producer if it is still running; it is a no-op after a completed Publication.
func (c *Client) Close() {
	if c.done {
		return
	}
	c.done = true
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	line, err := json.Marshal(request{JSONRPC: "2.0", ID: c.nextID, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", method, err)
	}
	if _, err := c.stdin.Write(append(line, '\n')); err != nil {
		return nil, c.failure("send %s: %v", method, err)
	}
	reply, err := c.readLine(ctx, method)
	if err != nil {
		return nil, err
	}
	var decoded response
	if err := json.Unmarshal(reply, &decoded); err != nil {
		return nil, c.failure("%s answered with non-JSON on stdout: %v (%.200q)", method, err, reply)
	}
	if decoded.Error != nil {
		return nil, c.failure("%s: %w", method, decoded.Error)
	}
	if decoded.ID == nil || *decoded.ID != c.nextID {
		return nil, c.failure("%s answered with response id %v, expected %d", method, decoded.ID, c.nextID)
	}
	return decoded.Result, nil
}

func (c *Client) readLine(ctx context.Context, method string) ([]byte, error) {
	line, err := c.stdout.ReadBytes('\n')
	if err == nil || (errors.Is(err, io.EOF) && len(bytes.TrimSpace(line)) > 0) {
		return line, nil
	}
	if ctx.Err() != nil {
		c.Close()
		return nil, fmt.Errorf("producer %s: %s interrupted: %w", c.binary.Path, method, ctx.Err())
	}
	c.Close()
	return nil, c.failure("producer exited before answering %s: %v", method, err)
}

// failure reports a producer problem with the tail of its stderr, which holds its own diagnosis.
func (c *Client) failure(format string, args ...any) error {
	err := fmt.Errorf("producer %s: "+format, append([]any{c.binary.Path}, args...)...)
	if tail := strings.TrimSpace(c.stderr.String()); tail != "" {
		return fmt.Errorf("%w\nproducer stderr (tail):\n%s", err, tail)
	}
	return err
}

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, p...)
	if len(t.data) > t.limit {
		t.data = t.data[len(t.data)-t.limit:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.data)
}
