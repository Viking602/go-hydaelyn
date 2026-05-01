package ports

type Command interface {
	CommandName() string
}
