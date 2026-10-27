package jellyfin

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// The worker's OpenSSL uses optimized ARM crypto. Only this private pipe sees
// HTTP; the helper verifies TLS and the server name before consuming any bytes.
// HTTP parsing, redirects, credentials and playlist policy stay in this package.
type tlsPipe struct {
	read, write *os.File
	cmd         *exec.Cmd
	remote      net.Addr
	once        sync.Once
}

type pipeAddress string

func (a pipeAddress) Network() string                 { return "tls" }
func (a pipeAddress) String() string                  { return string(a) }
func (p *tlsPipe) Read(b []byte) (int, error)         { return p.read.Read(b) }
func (p *tlsPipe) Write(b []byte) (int, error)        { return p.write.Write(b) }
func (p *tlsPipe) LocalAddr() net.Addr                { return pipeAddress("private-pipe") }
func (p *tlsPipe) RemoteAddr() net.Addr               { return p.remote }
func (p *tlsPipe) SetReadDeadline(t time.Time) error  { return p.read.SetReadDeadline(t) }
func (p *tlsPipe) SetWriteDeadline(t time.Time) error { return p.write.SetWriteDeadline(t) }
func (p *tlsPipe) SetDeadline(t time.Time) error {
	return errors.Join(p.SetReadDeadline(t), p.SetWriteDeadline(t))
}
func (p *tlsPipe) Close() error {
	p.once.Do(func() {
		p.write.Close()
		p.read.Close()
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
	})
	return nil
}

func tlsBridgeDial(binary string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		input, write, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		read, output, err := os.Pipe()
		if err != nil {
			input.Close()
			write.Close()
			return nil, err
		}
		cmd := exec.Command(binary, "--tls-connect", host, port)
		cmd.Stdin, cmd.Stdout = input, output
		// Never inherit legacy worker credentials or advertise a decoder owner.
		for _, field := range os.Environ() {
			if !strings.HasPrefix(field, "JFPLAY_HEADERS=") &&
				!strings.HasPrefix(field, "MISTERZINE_JFPLAY_OWNER=") &&
				!strings.HasPrefix(field, "MISTERZINE_PLEX_OWNER=") {
				cmd.Env = append(cmd.Env, field)
			}
		}
		err = cmd.Start()
		input.Close()
		output.Close()
		if err != nil {
			read.Close()
			write.Close()
			return nil, err
		}
		p := &tlsPipe{read: read, write: write, cmd: cmd, remote: pipeAddress(address)}
		if err := ctx.Err(); err != nil {
			p.Close()
			return nil, err
		}
		return p, nil
	}
}
