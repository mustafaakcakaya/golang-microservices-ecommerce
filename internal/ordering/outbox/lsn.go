// Package outbox reads committed outbox messages out of SQL Server's change
// data capture tables and tracks how far a reader has got.
//
// Nothing here publishes anything. Reading and publishing are separate so that
// the position in the stream is only ever advanced by code that knows a message
// was accepted, which is what makes delivery at-least-once rather than
// at-most-once.
package outbox

import (
	"bytes"
	"encoding/hex"
)

// LSNLength is the width of a log sequence number in SQL Server.
const LSNLength = 10

// LSN is a position in SQL Server's transaction log.
//
// It is an opaque binary value: the only meaningful operations are comparing
// two of them and asking the server for the next one. Every change committed in
// one transaction shares a start LSN, which is what lets a reader treat a
// transaction as a unit.
type LSN []byte

// IsZero reports whether the value is missing or all zeroes, which is how SQL
// Server says "no position".
func (l LSN) IsZero() bool {
	if len(l) == 0 {
		return true
	}

	for _, b := range l {
		if b != 0 {
			return false
		}
	}

	return true
}

// Compare orders two positions the way SQL Server orders them.
func (l LSN) Compare(other LSN) int {
	return bytes.Compare(l, other)
}

// String renders the position for logs.
func (l LSN) String() string {
	if len(l) == 0 {
		return "<none>"
	}

	return "0x" + hex.EncodeToString(l)
}
