package blackboard

import "github.com/Viking602/go-hydaelyn/internal/core/model"

type WriteItemCommand struct {
	Item model.BlackboardItem
}

func (WriteItemCommand) CommandName() string { return "blackboard.write_item" }
