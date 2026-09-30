package ledger

import "fmt"

// Direction indicates whether a posting debits or credits an account.
type Direction int8

// Valid Direction values.
const (
	Debit  Direction = 1
	Credit Direction = 2
)

func (d Direction) String() string {
	switch d {
	case Debit:
		return "DEBIT"
	case Credit:
		return "CREDIT"
	default:
		return fmt.Sprintf("Direction(%d)", int8(d))
	}
}

// IsValid reports whether d is a recognised Direction value.
func (d Direction) IsValid() bool {
	return d == Debit || d == Credit
}
