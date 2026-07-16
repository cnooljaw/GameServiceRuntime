package gsr

// CommandDeclarer declares the CommandIDs accepted by a Service.
type CommandDeclarer interface {
	Commands() []CommandID
}

// CommandRegistry validates the CommandIDs supported by one Service.
type CommandRegistry struct {
	commands map[CommandID]struct{}
}

// NewCommandRegistry creates an empty Command registry.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{commands: make(map[CommandID]struct{})}
}

// Register adds a stable CommandID.
func (r *CommandRegistry) Register(id CommandID) error {
	if _, exists := r.commands[id]; exists {
		return ErrCommandAlreadyRegistered
	}
	r.commands[id] = struct{}{}
	return nil
}

// Supports reports whether id is registered.
func (r *CommandRegistry) Supports(id CommandID) bool { _, ok := r.commands[id]; return ok }

func commandRegistryFor(service Service) (*CommandRegistry, error) {
	declarer, ok := service.(CommandDeclarer)
	if !ok {
		return nil, ErrInvalidServiceSpec
	}
	registry := NewCommandRegistry()
	for _, id := range declarer.Commands() {
		if err := registry.Register(id); err != nil {
			return nil, err
		}
	}
	if len(registry.commands) == 0 {
		return nil, ErrInvalidServiceSpec
	}
	return registry, nil
}
