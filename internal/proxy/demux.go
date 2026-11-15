package proxy

import (
	"bufio"
	"net"
)

// peekReader wraps a conn with a buffered reader that supports protocol
// demux without consuming bytes (interfaces §1.0: first byte 0x05 = SOCKS5,
// otherwise HTTP request line; peeked bytes are replayed to the parser).
type peekReader struct {
	br *bufio.Reader
}

func newPeekReader(conn net.Conn) *peekReader {
	return &peekReader{br: bufio.NewReaderSize(conn, 4096)}
}

// PeekByte returns the first byte of the stream without consuming it.
func (p *peekReader) PeekByte() (byte, error) {
	b, err := p.br.Peek(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}
