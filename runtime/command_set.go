package gsr

// CommandDeclarer declares the CommandIDs accepted by a Service.
type CommandDeclarer interface {
	Commands() []CommandID
}

type commandSet struct {
	commands map[CommandID]struct{}
}

func commandSetFor(service Service) (*commandSet, error) {
	declarer, ok := service.(CommandDeclarer)
	if !ok {
		return nil, ErrInvalidServiceSpec
	}
	commands := make(map[CommandID]struct{})
	for _, id := range declarer.Commands() {
		if _, exists := commands[id]; exists {
			return nil, ErrCommandAlreadyRegistered
		}
		commands[id] = struct{}{}
	}
	if len(commands) == 0 {
		return nil, ErrInvalidServiceSpec
	}
	return &commandSet{commands: commands}, nil
}

func (s *commandSet) supports(id CommandID) bool { _, ok := s.commands[id]; return ok }
