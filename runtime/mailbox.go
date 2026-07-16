package gsr

type mailbox struct{ queue chan Envelope }

func newMailbox(size int) *mailbox { return &mailbox{queue: make(chan Envelope, size)} }
func (m *mailbox) push(envelope Envelope) error {
	select {
	case m.queue <- envelope:
		return nil
	default:
		return ErrMailboxFull
	}
}
func (m *mailbox) pop() (Envelope, bool) {
	select {
	case value := <-m.queue:
		return value, true
	default:
		return Envelope{}, false
	}
}
func (m *mailbox) notEmpty() bool { return len(m.queue) > 0 }
