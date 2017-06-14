package imap

// Constants

// Integer counter for IMAP states.
const (
	Any State = iota
	NotAuthenticated
	Authenticated
	Mailbox
	Logout
)

// Structs

// State represents the integer value associated with one
// of the implemented IMAP states a connection can be in.
type State int

// Session contains all elements needed for tracking
// and performing the actual IMAP operations for an
// authenticated client.
type Session struct {
	State            State
	ClientID         string
	UserName         string
	RespWorker       string
	UserCRDTPath     string
	UserMaildirPath  string
	SelectedMailbox  string
	AppendInProgress bool
}
